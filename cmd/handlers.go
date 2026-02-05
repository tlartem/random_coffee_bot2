package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"example.com/random_coffee/database"
	"github.com/NicoNex/echotron/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

var adminChatIDsMap map[int64]bool

// sendMessage is a helper that sends a message and logs errors
func sendMessage(api echotron.API, text string, chatID int64) {
	if _, err := api.SendMessage(text, chatID, nil); err != nil {
		// Check if bot was blocked/kicked from chat
		errStr := err.Error()
		if strings.Contains(errStr, "bot was blocked") ||
			strings.Contains(errStr, "bot was kicked") ||
			strings.Contains(errStr, "chat not found") ||
			strings.Contains(errStr, "have no rights") {
			// Don't spam with errors - bot was removed from group
			if chatID < 0 {
				log.Warn().Err(err).Int64("group_id", chatID).Msg("Bot removed from group or no permissions")
			} else {
				log.Warn().Err(err).Int64("chat_id", chatID).Msg("Bot blocked by user or chat not found")
			}
		} else {
			// Real error
			if chatID < 0 {
				log.Error().Err(err).Int64("group_id", chatID).Msg("SendMessage failed")
			} else {
				log.Error().Err(err).Int64("chat_id", chatID).Msg("SendMessage failed")
			}
		}
	}
}

// getDisplayName returns username with @ prefix if available, otherwise returns full name
func getDisplayName(p database.Participant) string {
	if p.Username != "" {
		return "@" + p.Username
	}
	return p.FullName
}

// getWeekStart returns Monday of the current week in YYYY-MM-DD format
func getWeekStart(t time.Time) string {
	offset := int(t.Weekday() - time.Monday)
	if offset < 0 {
		offset += 7
	}
	return t.AddDate(0, 0, -offset).Format("2006-01-02")
}

func parseCommaSeparatedIDs(envKey, fieldName string) []int64 {
	value := os.Getenv(envKey)
	if value == "" {
		return []int64{}
	}

	ids := make([]int64, 0)
	for _, part := range strings.Split(value, ",") {
		id, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
		if err != nil {
			log.Warn().Err(err).Str("value", part).Str("field", fieldName).Msg("Failed to parse chat ID")
			continue
		}
		ids = append(ids, id)
	}
	return ids
}

func initAdmins() {
	adminChatIDsMap = make(map[int64]bool)
	for _, id := range parseCommaSeparatedIDs("ADMIN_CHAT_IDS", "admin") {
		adminChatIDsMap[id] = true
	}
}

func isAdmin(userID int64) bool {
	return adminChatIDsMap[userID]
}

func getConfiguredGroups() []int64 {
	return parseCommaSeparatedIDs("GROUP_CHAT_IDS", "group")
}

// HandlePollAnswer processes poll responses
func HandlePollAnswer(ctx context.Context, db *sql.DB, api echotron.API, pollAnswer *echotron.PollAnswer) {
	if pollAnswer.User == nil {
		return
	}

	// Try to find the poll in our database
	groupID, err := database.GetGroupIDByPollID(ctx, db, pollAnswer.PollID)
	if err != nil {
		// Poll not found - this is OK, it might be an old poll that was already processed
		// Don't spam logs with errors for old polls
		log.Debug().Str("poll_id", pollAnswer.PollID).Msg("Poll not found (probably old poll)")
		return
	}

	// If cancelled vote or selected "No" (option 1)
	if len(pollAnswer.OptionIDs) == 0 || pollAnswer.OptionIDs[0] != 0 {
		// Try to remove participant (ignore if not found)
		if err := database.DeleteParticipant(ctx, db, groupID, pollAnswer.User.ID); err != nil {
			log.Warn().Err(err).Int64("user_id", pollAnswer.User.ID).Int64("group_id", groupID).Msg("Failed to delete participant")
		} else {
			log.Info().Int64("user_id", pollAnswer.User.ID).Int64("group_id", groupID).Msg("User removed from participants")
		}
		return
	}

	// Selected "Yes" (option 0) - add participant
	fullName := pollAnswer.User.FirstName
	if pollAnswer.User.LastName != "" {
		fullName += " " + pollAnswer.User.LastName
	}

	p := database.Participant{
		ID:        uuid.New(),
		GroupID:   groupID,
		UserID:    pollAnswer.User.ID,
		Username:  pollAnswer.User.Username,
		FullName:  fullName,
		CreatedAt: time.Now(),
	}

	if err := database.CreateOrUpdateParticipant(ctx, db, p); err != nil {
		log.Error().Err(err).Int64("group_id", groupID).Int64("user_id", pollAnswer.User.ID).Msg("CreateOrUpdateParticipant failed")
		return
	}

	log.Info().Int64("user_id", pollAnswer.User.ID).Int64("group_id", groupID).Str("username", p.Username).Msg("User added to participants")
}

// HandleGroupCommand processes commands in group chats
func HandleGroupCommand(ctx context.Context, db *sql.DB, api echotron.API, message *echotron.Message) {
	if message.From == nil || !isAdmin(message.From.ID) {
		return
	}

	groupID := message.Chat.ID

	switch message.Text {
	case "/create_pairs":
		log.Info().Int64("group_id", groupID).Msg("Manual create_pairs command")
		CreatePairs(ctx, db, api, groupID)
	case "/send_quiz":
		log.Info().Int64("group_id", groupID).Msg("Manual send_quiz command")
		SendQuiz(ctx, db, api, groupID)
	}
}

// HandlePrivateCommand processes commands in private chats
func HandlePrivateCommand(ctx context.Context, db *sql.DB, api echotron.API, message *echotron.Message) {
	if message.From == nil {
		return
	}

	switch message.Text {
	case "/start":
		text := "👋 Привет! Это Random Coffee Bot.\n\n" +
			"Бот автоматически создает пары для случайных встреч.\n\n" +
			"📅 Расписание:\n" +
			"• Пятница 17:00 - рассылка опроса\n" +
			"• Воскресенье 19:00 - создание пар\n\n" +
			"Команды в личке (только для админов):\n" +
			"/groups - список групп\n\n" +
			"Команды в группе (только для админов):\n" +
			"/send_quiz - отправить опрос вручную\n" +
			"/create_pairs - создать пары вручную"
		sendMessage(api, text, message.Chat.ID)

	case "/groups":
		if !isAdmin(message.From.ID) {
			sendMessage(api, "❌ Доступ запрещен", message.Chat.ID)
			return
		}

		groupIDs := getConfiguredGroups()
		if len(groupIDs) == 0 {
			sendMessage(api, "Группы не настроены", message.Chat.ID)
			return
		}

		text := "Настроенные группы:\n"
		for _, gid := range groupIDs {
			text += fmt.Sprintf("• %d\n", gid)
		}
		sendMessage(api, text, message.Chat.ID)

	default:
		sendMessage(api, "Неизвестная команда. Используй /start для справки.", message.Chat.ID)
	}
}

// SendQuiz sends a poll to the group
func SendQuiz(ctx context.Context, db *sql.DB, api echotron.API, groupID int64) {
	// Clean up old poll mapping for this group if exists
	// This handles the case where a new poll is sent before pairs were created
	if err := database.DeletePollMapping(ctx, db, groupID); err != nil {
		log.Warn().Err(err).Int64("group_id", groupID).Msg("Failed to delete old poll mapping")
	}

	question := "Участвуешь в Random Coffee на этой неделе? ☕️"
	options := []echotron.InputPollOption{
		{Text: "Да!"},
		{Text: "Нет"},
	}
	opts := &echotron.PollOptions{
		IsAnonymous:           false,
		AllowsMultipleAnswers: false,
	}

	result, err := api.SendPoll(groupID, question, options, opts)
	if err != nil {
		log.Error().Err(err).Int64("group_id", groupID).Msg("SendPoll failed")
		return
	}

	if result.Result == nil || result.Result.Poll == nil {
		log.Error().Int64("group_id", groupID).Msg("Poll result is nil")
		return
	}

	messageID := result.Result.ID

	pm := database.PollMapping{
		PollID:    result.Result.Poll.ID,
		GroupID:   groupID,
		MessageID: int64(messageID),
	}

	if err := database.CreatePollMapping(ctx, db, pm); err != nil {
		log.Error().Err(err).Str("poll_id", pm.PollID).Msg("CreatePollMapping failed")
		return
	}

	// Pin the poll message
	_, err = api.PinChatMessage(groupID, messageID, &echotron.PinMessageOptions{DisableNotification: true})
	if err != nil {
		log.Warn().Err(err).Int64("group_id", groupID).Int("message_id", messageID).Msg("PinChatMessage failed (check bot permissions)")
		// Don't return - poll was sent successfully
	}

	log.Info().Str("poll_id", pm.PollID).Int64("group_id", groupID).Int("message_id", messageID).Msg("Quiz sent and pinned successfully")
}

// filterUniquePairs selects pairs where each participant appears only once
func filterUniquePairs(availablePairs [][2]database.Participant) ([][2]database.Participant, map[int64]bool) {
	usedUsers := make(map[int64]bool)
	finalPairs := make([][2]database.Participant, 0)

	for _, pair := range availablePairs {
		p1, p2 := pair[0], pair[1]
		if !usedUsers[p1.UserID] && !usedUsers[p2.UserID] {
			finalPairs = append(finalPairs, pair)
			usedUsers[p1.UserID] = true
			usedUsers[p2.UserID] = true
		}
	}
	return finalPairs, usedUsers
}

// savePairsToDatabase saves pairs to database for current week
func savePairsToDatabase(ctx context.Context, db *sql.DB, finalPairs [][2]database.Participant, groupID int64) error {
	weekStart := getWeekStart(time.Now())
	pairs := make([]database.Pair, 0, len(finalPairs))

	for _, fp := range finalPairs {
		pairs = append(pairs, database.Pair{
			ID:        uuid.New(),
			GroupID:   groupID,
			WeekStart: weekStart,
			User1ID:   fp[0].UserID,
			User2ID:   fp[1].UserID,
			CreatedAt: time.Now(),
		})
	}

	return database.CreatePairs(ctx, db, pairs)
}

// buildPairsMessage creates formatted message with pairs list
func buildPairsMessage(finalPairs [][2]database.Participant) string {
	message := "🎉 Пары Random Coffee на эту неделю ☕️\n\n"
	for _, pair := range finalPairs {
		p1, p2 := pair[0], pair[1]
		message += fmt.Sprintf("▫️ %s ✖️ %s\n\n", getDisplayName(p1), getDisplayName(p2))
	}
	message += "💬 Напиши прямо сейчас собеседнику в личку и договорись о месте и времени!"
	return message
}

// appendUnpairedMessage adds list of unpaired participants to message
func appendUnpairedMessage(ctx context.Context, db *sql.DB, message string, groupID int64, usedUsers map[int64]bool) string {
	allParticipants, err := database.GetAllParticipants(ctx, db, groupID)
	if err != nil {
		log.Error().Err(err).Int64("group_id", groupID).Msg("GetAllParticipants failed")
		return message
	}

	if len(allParticipants) == 0 {
		return message
	}

	unpaired := make([]database.Participant, 0)
	for _, p := range allParticipants {
		if !usedUsers[p.UserID] {
			unpaired = append(unpaired, p)
		}
	}

	if len(unpaired) == 0 {
		return message
	}

	message += "\n\n😔 К сожалению, без пары: "
	names := make([]string, 0, len(unpaired))
	for _, p := range unpaired {
		names = append(names, getDisplayName(p))
	}
	message += strings.Join(names, ", ")
	return message
}

// CreatePairs generates random pairs
func CreatePairs(ctx context.Context, db *sql.DB, api echotron.API, groupID int64) {
	availablePairs, err := database.GetAvailablePairs(ctx, db, groupID)
	if err != nil {
		log.Error().Err(err).Int64("group_id", groupID).Msg("GetAvailablePairs failed")
		sendMessage(api, "❌ Ошибка при получении доступных пар", groupID)
		return
	}

	if len(availablePairs) == 0 {
		sendMessage(api, "❌ Недостаточно участников или нет уникальных пар", groupID)
		return
	}

	finalPairs, usedUsers := filterUniquePairs(availablePairs)
	if len(finalPairs) == 0 {
		sendMessage(api, "❌ Не удалось создать уникальные пары", groupID)
		return
	}

	if err = savePairsToDatabase(ctx, db, finalPairs, groupID); err != nil {
		log.Error().Err(err).Int64("group_id", groupID).Msg("CreatePairs failed")
		sendMessage(api, "❌ Ошибка при сохранении пар", groupID)
		return
	}

	message := buildPairsMessage(finalPairs)
	message = appendUnpairedMessage(ctx, db, message, groupID, usedUsers)

	// Check message length (Telegram limit is 4096 characters)
	if len(message) > 4000 {
		// Truncate at a reasonable boundary
		message = message[:4000] + "\n\n...(обрезано)"
	}

	sendMessage(api, message, groupID)

	// Unpin the poll message
	pollMapping, err := database.GetPollMappingByGroupID(ctx, db, groupID)
	if err != nil {
		log.Warn().Err(err).Int64("group_id", groupID).Msg("GetPollMappingByGroupID failed (no active poll)")
	} else if pollMapping != nil {
		_, err = api.UnpinChatMessage(groupID, &echotron.UnpinMessageOptions{MessageID: int(pollMapping.MessageID)})
		if err != nil {
			log.Warn().Err(err).Int64("group_id", groupID).Int64("message_id", pollMapping.MessageID).Msg("UnpinChatMessage failed (check bot permissions)")
		} else {
			log.Info().Int64("group_id", groupID).Int64("message_id", pollMapping.MessageID).Msg("Poll message unpinned")
		}

		// Delete poll mapping after attempting to unpin (even if unpin failed)
		if err := database.DeletePollMapping(ctx, db, groupID); err != nil {
			log.Warn().Err(err).Int64("group_id", groupID).Msg("DeletePollMapping failed")
		}
	}

	if err = database.ClearAllParticipants(ctx, db, groupID); err != nil {
		log.Error().Err(err).Int64("group_id", groupID).Msg("ClearAllParticipants failed")
	}

	log.Info().Int64("group_id", groupID).Int("pairs_count", len(finalPairs)).Msg("Pairs created successfully")
}

func SendQuizToAllGroups(ctx context.Context, db *sql.DB, api echotron.API) {
	groups := getConfiguredGroups()
	if len(groups) == 0 {
		log.Warn().Msg("No groups configured in GROUP_CHAT_IDS")
		return
	}

	log.Info().Int("groups_count", len(groups)).Msg("Sending quiz to all groups")
	for _, groupID := range groups {
		SendQuiz(ctx, db, api, groupID)
	}
}

func CreatePairsForAllGroups(ctx context.Context, db *sql.DB, api echotron.API) {
	groups := getConfiguredGroups()
	if len(groups) == 0 {
		log.Warn().Msg("No groups configured in GROUP_CHAT_IDS")
		return
	}

	log.Info().Int("groups_count", len(groups)).Msg("Creating pairs for all groups")
	for _, groupID := range groups {
		CreatePairs(ctx, db, api, groupID)
	}
}

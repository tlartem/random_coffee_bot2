# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Random Coffee Bot is a Telegram bot that organizes random coffee meetings between participants. It automatically creates pairs, sends polls to collect participation, and notifies users about their matches. The bot runs on a scheduled basis using APScheduler.

**Multi-Group Support**: The bot supports multiple Telegram groups simultaneously, with isolated participant pools and pair tracking per group.

## Development Commands

### Docker Development (Primary Method)

```bash
# Build the Docker image
docker-compose build

# Start the bot in detached mode
docker-compose up -d

# View logs
docker-compose logs randomcoffee_bot
docker-compose logs -f randomcoffee_bot  # Follow logs

# Stop the bot
docker-compose down

# Rebuild and restart
docker-compose up --build

# Clean up all resources
docker-compose down --rmi all --volumes --remove-orphans
```

### Direct Python Execution

The bot uses `uv` for dependency management (see `uv.lock`). To run directly:

```bash
# Install dependencies
uv sync

# Run the bot
python -m src.main
```

### Testing Bot Commands

The bot responds to these Telegram commands:
- `/create_pairs` - Manually trigger pair creation
- `/send_quiz` - Manually send participation quiz

## Architecture

### Layered Architecture

The project follows a clean architecture pattern with clear separation of concerns:

```
src/
├── adapter/          # External interfaces (Telegram, Database)
├── controller/       # Request handlers and scheduling
├── usecase/          # Business logic
├── model/            # Domain models (SQLAlchemy)
└── config.py         # Configuration using Pydantic Settings
```

### Adapter Layer
- **telegram/** - Telegram Bot API integration using aiogram 3.x
  - `connection.py`: Bot instance
  - `routes.py`: Message/poll sending utilities
- **database/** - SQLAlchemy async database operations
  - `connection.py`: Database helper initialization
  - `participant.py`: Participant CRUD operations (filtered by group_id)
  - `pair.py`: Pair CRUD and matching logic (filtered by group_id)
  - `poll_mapping.py`: Maps poll IDs to group IDs for poll answer handling

### Controller Layer
- **telegram_callback.py**: Aiogram router with command handlers and poll answer handlers
  - Extracts group_id from message.chat.id for commands
  - Uses poll_mapping to find group_id for poll answers
- **scheduler.py**: APScheduler configuration
  - Sends quiz to all groups every Friday at 17:00 Moscow time
  - Creates pairs for all groups every Sunday at 19:00 Moscow time

### Use Case Layer
- **send_quiz.py**: Sends participation poll to specific group and stores poll_id -> group_id mapping
- **create_pairs.py**: Generates unique pairs from available participants for specific group
- **handle_quiz_answer.py**: Processes poll responses for specific group

### Model Layer
- Based on SQLAlchemy 2.0 with async support
- **base.py**: Declarative base with auto table naming
- **participant.py**: Participant model with group_id
- **pair.py**: Pair model with group_id and week tracking, unique constraint on (group_id, week_start, user1_id, user2_id)
- **poll_mapping.py**: Poll ID to group ID mapping for tracking poll sources

### Shared Utilities
Located in `shared/` directory:
- **db_helper.py**: Reusable `DatabaseHelper` class for SQLAlchemy async session management
- **logger.py**: Logging configuration with admin notifications

## Configuration

Configuration uses Pydantic Settings with environment variables:

```python
# Required environment variables (see .env.example)
bot__token=<telegram_bot_token>
bot__group_chat_ids='[group_id1, group_id2, ...]'  # JSON list of group IDs (negative numbers)
bot__admin_chat_id=<admin_user_id>
db__url=<database_url>  # PostgreSQL or SQLite URL
```

**Multi-Group Configuration**: The bot now supports multiple groups via `bot__group_chat_ids` as a JSON list. Each group operates independently with its own participant pool and pair history.

## Database

- Supports both SQLite (for local dev) and PostgreSQL
- Uses SQLAlchemy 2.0 async API
- Database file `random_coffee.db` is mounted as volume in Docker
- Tables auto-created on startup via `database.create_tables()`
- Session management through `database.session_getter()` context manager

## Scheduling

APScheduler (AsyncIOScheduler) runs timezone-aware jobs:
- Timezone: Europe/Moscow
- Jobs defined in `src/controller/scheduler.py`
- All jobs removed and re-added on startup to avoid duplicates

## Key Workflows

### Poll → Pair Creation Flow (Per Group)
1. Friday 17:00: Quiz sent to all configured groups asking about participation
2. Poll ID → group ID mapping stored in poll_mapping table
3. Users respond to poll (anonymous=False to track users)
4. Responses handled by `handle_quiz_answer` use case → poll_id looked up to find group_id → participant stored with group_id
5. Sunday 19:00: `create_pairs` use case runs for each group independently
6. Available participants matched using logic in `adapter/database/pair.py` (filtered by group_id)
7. Pairs posted to respective group with usernames
8. Participants table cleared for that group for next week

### Pair Matching Logic
Located in `src/adapter/database/pair.py`:
- Ensures unique pairings (checks historical pairs for that group_id)
- Filters participants by group_id
- Uses SQL queries with aliases to find valid combinations
- Randomizes pair selection

## Important Notes

- All Telegram operations use aiogram 3.x async API
- Logging configured to file `shared/logs/bot.log` with admin notifications on errors
- Bot uses `skip_updates=True` to ignore messages received while offline
- Router registered in main.py, actual handlers in `controller/telegram_callback.py`
- The main entry point expects `router` to be imported from `src.controller.tg_handlers`, but the actual file is `telegram_callback.py` (verify import path)

### Multi-Group Implementation
- All database queries filter by `group_id` to isolate data per group
- Poll answers don't contain chat_id, so poll_id → group_id mapping is stored when quiz is sent
- Scheduler iterates over all configured groups for scheduled tasks
- Commands (like `/create_pairs`, `/send_quiz`) extract group_id from message.chat.id
- Each group maintains independent participant pools and pair history

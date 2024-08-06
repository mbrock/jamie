#!/bin/bash

# Constants
JAMIE_USERNAME="jamie"
JAMIE_PASSWORD="jamie"
DB_NAME="jamie"

# Logging function
log() {
    echo "LOG ($1): $2"
}

# Run command with optional sudo
run_command() {
    local cmd=("$@")
    echo "Would run command: ${cmd[*]}"
    read -p "Succeed or fail? (s/f): " response
    if [[ $response == "s" ]]; then
        return 0
    elif [[ $response == "f" ]]; then
        if confirm "Use sudo?"; then
            echo "Would run command with sudo: sudo ${cmd[*]}"
            read -p "Succeed or fail? (s/f): " sudo_response
            [[ $sudo_response == "s" ]] && return 0 || return 1
        else
            return 1
        fi
    else
        log "error" "Invalid response"
        return 1
    fi
}

# Confirm function
confirm() {
    read -p "$1 (y/n): " response
    [[ $response == "y" ]]
}

# Ensure system user exists
ensure_system_user() {
    if id "$JAMIE_USERNAME" &>/dev/null; then
        log "info" "System user already exists"
    else
        log "info" "Creating system user"
        if run_command useradd -r -s /bin/false "$JAMIE_USERNAME"; then
            log "info" "System user created successfully"
        else
            log "error" "Failed to create system user"
            exit 1
        fi
    fi
}

# Ensure database user exists
ensure_database_user() {
    if run_command psql -tAc "SELECT 1 FROM pg_roles WHERE rolname='$JAMIE_USERNAME'"; then
        log "info" "Database user already exists"
    else
        log "info" "Creating database user"
        if run_command createuser -s "$JAMIE_USERNAME" && \
           run_command psql -c "ALTER USER $JAMIE_USERNAME WITH PASSWORD '$JAMIE_PASSWORD';"; then
            log "info" "Database user created successfully"
        else
            log "error" "Failed to create database user or set password"
            exit 1
        fi
    fi
}

# Ensure database exists
ensure_database() {
    if run_command psql -lqt | cut -d \| -f 1 | grep -cw "$DB_NAME"; then
        log "info" "Database already exists"
    else
        log "info" "Creating database"
        if run_command createdb -O "$JAMIE_USERNAME" "$DB_NAME"; then
            log "info" "Database created successfully"
            log "info" "Initializing database schema..."
            if run_command psql -d "$DB_NAME" -f db/db_init.sql; then
                log "info" "Database schema initialized successfully"
            else
                log "error" "Failed to initialize database schema"
                exit 1
            fi
        else
            log "error" "Failed to create database"
            exit 1
        fi
    fi
}

# Prompt for API keys
prompt_for_api_keys() {
    local services=("Discord" "Google Cloud (Gemini)" "Speechmatics")
    local keys=("DISCORD_TOKEN" "GEMINI_API_KEY" "SPEECHMATICS_API_KEY")

    for i in "${!services[@]}"; do
        read -p "Enter API key for ${services[$i]}: " value
        echo "Setting config: ${keys[$i]} = $value"
    done
}

# Main setup function
run_setup() {
    log "info" "Starting Jamie setup..."
    ensure_system_user
    ensure_database_user
    ensure_database
    log "info" "Simulating database opening."
    read -p "Succeed or fail? (s/f): " response
    if [[ $response == "s" ]]; then
        prompt_for_api_keys
        log "info" "Setup completed successfully!"
    else
        log "error" "Failed to open database"
        exit 1
    fi
}

# Run the setup
run_setup

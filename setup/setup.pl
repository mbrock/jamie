% Mock primitive operations (to be implemented by the runtime)
:- dynamic log/2.
:- dynamic run_command/2.
:- dynamic open_database/1.
:- dynamic set_config/2.

% Constants
jamie_username('jamie').
jamie_password('jamie').
db_name('jamie').

% Main setup predicate
run_setup :-
    log(info, 'Starting Jamie setup...'),
    ensure_system_user,
    ensure_database_user,
    ensure_database,
    open_database(Result),
    (Result = success ->
        log(info, 'Successfully connected to the database'),
        prompt_for_api_keys,
        save_configuration
    ;
        log(fatal, 'Failed to connect to the database')
    ),
    log(info, 'Setup completed successfully!').

% Ensure system user exists
ensure_system_user :-
    jamie_username(Username),
    system_user_exists(Username),
    log(info, 'System user already exists'),
    !.
ensure_system_user :-
    jamie_username(Username),
    log(info, 'Creating system user'),
    create_system_user(Username),
    system_user_exists(Username).

create_system_user(Username) :-
    run_command(['useradd', '-r', '-s', '/bin/false', Username]),
    log(info, 'System user created successfully').
create_system_user(Username) :-
    log(warn, 'Failed to create user, retrying with sudo'),
    run_command(['sudo', 'useradd', '-r', '-s', '/bin/false', Username]),
    log(info, 'System user created successfully with sudo').

% Ensure database user exists
ensure_database_user :-
    jamie_username(Username),
    database_user_exists(Username),
    log(info, 'Database user already exists'),
    !.
ensure_database_user :-
    jamie_username(Username),
    jamie_password(Password),
    log(info, 'Creating database user'),
    create_database_user(Username, Password),
    database_user_exists(Username).

create_database_user(Username, Password) :-
    run_command(['createuser', '-s', Username]),
    set_database_user_password(Username, Password).
create_database_user(Username, Password) :-
    log(warn, 'Failed to create database user, retrying with sudo'),
    run_command(['sudo', '-u', 'postgres', 'createuser', '-s', Username]),
    set_database_user_password(Username, Password).

set_database_user_password(Username, Password) :-
    format(atom(AlterCommand), 'ALTER USER ~w WITH PASSWORD \'~w\';', [Username, Password]),
    run_command(['psql', '-c', AlterCommand]),
    log(info, 'Database user created successfully').
set_database_user_password(Username, Password) :-
    format(atom(AlterCommand), 'ALTER USER ~w WITH PASSWORD \'~w\';', [Username, Password]),
    log(warn, 'Failed to set database user password, retrying with sudo'),
    run_command(['sudo', '-u', 'postgres', 'psql', '-c', AlterCommand]),
    log(info, 'Database user created successfully with sudo').

% Ensure database exists
ensure_database :-
    db_name(DbName),
    database_exists(DbName),
    log(info, 'Database already exists'),
    !.
ensure_database :-
    db_name(DbName),
    jamie_username(Owner),
    log(info, 'Creating database'),
    create_database(DbName, Owner),
    database_exists(DbName).

create_database(DbName, Owner) :-
    run_command(['createdb', '-O', Owner, DbName]),
    log(info, 'Database created successfully'),
    initialize_database_schema(DbName, Owner).
create_database(DbName, Owner) :-
    log(warn, 'Failed to create database, retrying with sudo'),
    run_command(['sudo', '-u', 'postgres', 'createdb', '-O', Owner, DbName]),
    log(info, 'Database created successfully with sudo'),
    initialize_database_schema(DbName, Owner).

initialize_database_schema(DbName, Owner) :-
    log(info, 'Initializing database schema...'),
    run_command(['psql', '-d', DbName, '-f', 'db/db_init.sql']),
    log(info, 'Database schema initialized successfully').
initialize_database_schema(DbName, Owner) :-
    log(warn, 'Failed to initialize database schema, retrying with sudo'),
    run_command(['sudo', '-u', Owner, 'psql', '-d', DbName, '-f', 'db/db_init.sql']),
    log(info, 'Database schema initialized successfully with sudo').

% Helper predicates to check existence
system_user_exists(Username) :-
    run_command(['id', Username], success).

database_user_exists(Username) :-
    format(atom(Query), 'SELECT 1 FROM pg_roles WHERE rolname=\'~w\'', [Username]),
    run_command(['psql', '-tAc', Query], '1\n').

database_exists(DbName) :-
    run_command(['psql', '-lqt', '|', 'cut', '-d', '|', '-f', '1', '|', 'grep', '-cw', DbName], '1\n').

% Run command with potential sudo
run_command(Command) :-
    run_command(Command, success).
run_command(Command) :-
    append(['sudo'], Command, SudoCommand),
    run_command(SudoCommand, success).

% Prompt for API keys
prompt_for_api_keys :-
    write('Enter your Discord Bot Token: '), read(DiscordToken),
    write('Enter your Google Cloud (Gemini) API Key: '), read(GeminiApiKey),
    write('Enter your Speechmatics API Key: '), read(SpeechmaticsApiKey),
    set_config('DISCORD_TOKEN', DiscordToken),
    set_config('GEMINI_API_KEY', GeminiApiKey),
    set_config('SPEECHMATICS_API_KEY', SpeechmaticsApiKey).

% Save configuration
save_configuration :-
    log(info, 'Configuration saved successfully').

% Entry point
:- run_setup.

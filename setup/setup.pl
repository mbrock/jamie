:- dynamic log/2.
:- dynamic run_command/2.
:- dynamic open_database/1.
:- dynamic set_config/2.
:- dynamic ask/2.
:- dynamic confirm/2.

jamie_username('jamie').
jamie_password('jamie').
db_name('jamie').

run_setup :-
	log(info, 'Starting Jamie setup...'), ensure_system_user, !, ensure_database_user, !, ensure_database, !, 
	open_database(success), prompt_for_api_keys.

ensure_system_user :-
	jamie_username(Username), 
	system_user_exists(Username), 
	log(info, 'System user already exists').

ensure_system_user :-
	jamie_username(Username), 
	log(info, 'Creating system user'), 
	create_system_user(Username), 
	system_user_exists(Username).

create_system_user(Username) :-
	run_command(['useradd', '-r', '-s', '/bin/false', Username]), 
	log(info, 'System user created successfully').

ensure_database_user :-
	jamie_username(Username), 
	database_user_exists(Username).

ensure_database_user :-
	jamie_username(Username), 
	jamie_password(Password), 
	log(info, 'Creating database user'), 
	create_database_user(Username, Password), 
	database_user_exists(Username).

create_database_user(Username, Password) :-
	run_command(['createuser', '-s', Username]), 
	set_database_user_password(Username, Password).

set_database_user_password(Username, Password) :-
	format(
		atom(AlterCommand), 'ALTER USER ~w WITH PASSWORD \'~w\';', [Username, Password]), 
	run_command(['psql', '-c', AlterCommand]), 
	log(info, 'Database user created successfully').

ensure_database :-
	db_name(DbName), 
	database_exists(DbName), 
	log(info, 'Database already exists').

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

initialize_database_schema(DbName, Owner) :-
	log(info, 'Initializing database schema...'), 
	run_command(['psql', '-d', DbName, '-f', 'db/db_init.sql']), 
	log(info, 'Database schema initialized successfully').

% Helper predicates to check existence

system_user_exists(Username) :-
	run_command(['id', Username], success).

database_user_exists(Username) :-
	format(
		atom(Query), 'SELECT 1 FROM pg_roles WHERE rolname=\'~w\'', [Username]), 
	run_command(['psql', '-tAc', Query], '1\n').

database_exists(DbName) :-
	run_command(['psql', '-lqt', '|', 'cut', '-d', '|', '-f', '1', '|', 'grep', '-cw', DbName], '1\n').

% Run command with potential sudo

run_command(Command) :-
	run_command(Command, success).

run_command(Command) :-
	confirm('Use sudo?', yes), 
	append(['sudo'], Command, SudoCommand), 
	run_command(SudoCommand, success).

prompt_for_api_keys :-
	prompt_for_api_key('Discord', 'DISCORD_TOKEN'), 
	prompt_for_api_key('Google Cloud (Gemini)', 'GEMINI_API_KEY'), 
	prompt_for_api_key('Speechmatics', 'SPEECHMATICS_API_KEY').

prompt_for_api_key(Service, Key) :-
	format(
		atom(Prompt), 'Enter API key for ~w: ', [Service]), 
	ask(Prompt, Value), 
	set_config(Key, Value).

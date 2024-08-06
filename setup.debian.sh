#!/bin/bash

set -ex

sudo adduser --disabled-password jamie
sudo -u postgres psql -c "CREATE USER jamie WITH PASSWORD 'jamie';"
sudo -u postgres psql -c "CREATE DATABASE jamie OWNER jamie;"
sudo -u postgres psql -d jamie -f db/db_init.sql


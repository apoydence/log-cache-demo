#!/bin/bash

set -eu

app_name=promql-plotter
pwd=$PWD
PROJECT_DIR="$(cd "$(dirname "$0")/../.."; pwd)"

# Ensure we are starting from the project directory
cd $PROJECT_DIR

TEMP_DIR=$(mktemp -d)

function fail {
    echo $1
    exit 1
}

# promql-plotter binary
echo "building promql-plotter binary..."
GOOS=linux go get -d ./cmd/promql-plotter &> /dev/null || fail "failed to get/download promql-plotter"
GOOS=linux go build -o $TEMP_DIR/promql-plotter ./cmd/promql-plotter &> /dev/null || fail "failed to build promql-plotter"
cp cmd/promql-plotter/run.sh $TEMP_DIR
echo "done building promql-plotter binary."

# CF-Space-Security binaries
echo "building CF-Space-Security binaries..."
go get github.com/apoydence/cf-space-security/... &> /dev/null || fail "failed to get cf-space-security"
GOOS=linux go build -o $TEMP_DIR/proxy ../cf-space-security/cmd/proxy &> /dev/null || fail "failed to build cf-space-security proxy"
echo "done building CF-Space-Security binaries."

echo "pushing $app_name..."
cf push $app_name --no-start -p $TEMP_DIR -b binary_buildpack -c ./run.sh &> /dev/null || fail "failed to push app $app_name"
echo "done pushing $app_name."

if [ -z ${CF_HOME+x} ]; then
    CF_HOME=$HOME
fi

# Configure
echo "configuring $app_name..."
cf set-env $app_name REFRESH_TOKEN "$(cat $CF_HOME/.cf/config.json | jq -r .RefreshToken)" &> /dev/null || fail "failed to set REFRESH_TOKEN"
cf set-env $app_name CLIENT_ID "$(cat $CF_HOME/.cf/config.json | jq -r .UAAOAuthClient)" &> /dev/null || fail "failed to set set CLIENT_ID"

skip_ssl_validation="$(cat $CF_HOME/.cf/config.json | jq -r .SSLDisabled)"
if [ $skip_ssl_validation = "true" ]; then
    cf set-env $app_name SKIP_SSL_VALIDATION true &> /dev/null || fail "failed to set SKIP_SSL_VALIDATION"
fi

echo "done configuring $app_name."

echo "starting $app_name..."
cf start $app_name &> /dev/null || fail "failed to start $app_name"
echo "done starting $app_name."

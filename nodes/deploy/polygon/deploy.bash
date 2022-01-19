#!/usr/bin/env bash

# Deployment script - intended to run on Moonstream Polygon node control server

# Colors
C_RESET='\033[0m'
C_RED='\033[1;31m'
C_GREEN='\033[1;32m'
C_YELLOW='\033[1;33m'

# Logs
PREFIX_INFO="${C_GREEN}[INFO]${C_RESET} [$(date +%d-%m\ %T)]"
PREFIX_WARN="${C_YELLOW}[WARN]${C_RESET} [$(date +%d-%m\ %T)]"
PREFIX_CRIT="${C_RED}[CRIT]${C_RESET} [$(date +%d-%m\ %T)]"

# Main
AWS_DEFAULT_REGION="${AWS_DEFAULT_REGION:-us-east-1}"
APP_DIR="${APP_DIR:-/home/ubuntu/moonstream}"
APP_NODES_DIR="${APP_DIR}/nodes"
SECRETS_DIR="${SECRETS_DIR:-/home/ubuntu/moonstream-secrets}"
PARAMETERS_ENV_PATH="${SECRETS_DIR}/app.env"
SCRIPT_DIR="$(realpath $(dirname $0))"
BLOCKCHAIN="polygon"
HEIMDALL_HOME="/mnt/disks/nodes/${BLOCKCHAIN}/.heimdalld"

# Node status server service file
NODE_STATUS_SERVER_SERVICE_FILE="node-status.service"

set -eu

echo
echo
echo -e "${PREFIX_INFO} Building executable server of node status server"
EXEC_DIR=$(pwd)
cd "${APP_NODES_DIR}/server"
HOME=/root /usr/local/go/bin/go build -o "${APP_NODES_DIR}/server/nodestatus" "${APP_NODES_DIR}/server/main.go"
cd "${EXEC_DIR}"

echo
echo
echo -e "${PREFIX_INFO} Install checkenv"
HOME=/root /usr/local/go/bin/go install github.com/bugout-dev/checkenv@latest

echo
echo
echo -e "${PREFIX_INFO} Retrieving deployment parameters"
mkdir -p "${SECRETS_DIR}"
> "${PARAMETERS_ENV_PATH}"
HOME=/root AWS_DEFAULT_REGION="${AWS_DEFAULT_REGION}" $HOME/go/bin/checkenv show aws_ssm+Product:moonstream,Node:true >> "${PARAMETERS_ENV_PATH}"

echo
echo
echo -e "${PREFIX_INFO} Add instance local IP to parameters"
AWS_LOCAL_IPV4="$(ec2metadata --local-ipv4)"
echo "AWS_LOCAL_IPV4=$AWS_LOCAL_IPV4" >> "${PARAMETERS_ENV_PATH}"

echo
echo
echo -e "${PREFIX_INFO} Replacing existing node status server definition with ${NODE_STATUS_SERVER_SERVICE_FILE}"
chmod 644 "${SCRIPT_DIR}/${NODE_STATUS_SERVER_SERVICE_FILE}"
cp "${SCRIPT_DIR}/${NODE_STATUS_SERVER_SERVICE_FILE}" "/etc/systemd/system/${NODE_STATUS_SERVER_SERVICE_FILE}"
systemctl daemon-reload
systemctl restart "${NODE_STATUS_SERVER_SERVICE_FILE}"
systemctl status "${NODE_STATUS_SERVER_SERVICE_FILE}"

echo
echo
echo -e "${PREFIX_INFO} Source extracted parameters"
. "${PARAMETERS_ENV_PATH}"

echo
echo
echo -e "${PREFIX_INFO} Retrieving Ethereum node address"
RETRIEVED_NODE_ETHEREUM_IP_ADDR=$(aws route53 list-resource-record-sets --hosted-zone-id "${MOONSTREAM_INTERNAL_HOSTED_ZONE_ID}" --query "ResourceRecordSets[?Name == 'a.ethereum.moonstream.internal.'].ResourceRecords[].Value" | jq -r .[0])
if [ "$RETRIEVED_NODE_ETHEREUM_IP_ADDR" == "null" ]; then
  verbose "${PREFIX_CRIT} Ethereum node internal DNS record address is null"
  exit 1
fi

echo
echo
MOONSTREAM_NODE_ETHEREUM_IPC_URI="http://$RETRIEVED_NODE_ETHEREUM_IP_ADDR:8545"
echo -e "${PREFIX_INFO} Update heimdall config file with Ethereum URI ${C_GREEN}${MOONSTREAM_NODE_ETHEREUM_IPC_URI}${C_RESET}"
sed -i "s|^eth_rpc_url =.*|eth_rpc_url = \"$MOONSTREAM_NODE_ETHEREUM_IPC_URI\"|" "${HEIMDALL_HOME}/config/heimdall-config.toml"
echo -e "${PREFIX_INFO} Updated ${C_GREEN}eth_rpc_url = $MOONSTREAM_NODE_ETHEREUM_IPC_URI${C_RESET} for heimdall"

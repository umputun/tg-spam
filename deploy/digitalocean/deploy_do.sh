#!/bin/bash

# TG-Spam DigitalOcean One-Click Deployment Script

# --- Configuration ---
# You can change these defaults if needed
DEFAULT_DROPLET_SIZE="s-1vcpu-1gb" # Smallest recommended size with Docker support
# Check DigitalOcean Marketplace for the latest Ubuntu LTS with Docker pre-installed (e.g., docker-22-04, docker-24-04)
DEFAULT_DROPLET_IMAGE="docker-24-04" # Ubuntu 24.04 with Docker pre-installed (verify availability)
DEFAULT_DROPLET_NAME_PREFIX="tg-spam-do"
TG_SPAM_INSTALL_PATH="/opt/tg-spam"

# --- Helper Functions ---
echo_info() {
    echo -e "\033[1;34m[INFO]\033[0m $1"
}

echo_success() {
    echo -e "\033[1;32m[SUCCESS]\033[0m $1"
}

echo_warning() {
    echo -e "\033[1;33m[WARNING]\033[0m $1"
}

echo_error() {
    echo -e "\033[1;31m[ERROR]\033[0m $1"
}

check_command() {
    if ! command -v "$1" &> /dev/null; then
        echo_error "Command '$1' not found. Please install it and make sure it's in your PATH."
        if [ "$1" == "doctl" ]; then
            echo_info "doctl installation guide: https://docs.digitalocean.com/reference/doctl/how-to/install/"
        fi
        exit 1
    fi
}

# --- Main Script ---

# 0. Check for doctl
check_command "doctl"

# 1. Gather User Input
echo_info "Welcome to the TG-Spam DigitalOcean Deployment Script!"
echo_info "This script will guide you through deploying TG-Spam to a new DigitalOcean Droplet."

# Get Telegram Bot Token
echo_info "\n--- Telegram Bot Token ---"
echo_info "You need a token for your Telegram Bot."
echo_info "1. Open Telegram and search for \"@BotFather\"."
echo_info "2. Start a chat with BotFather and send the command /newbot."
echo_info "3. Follow the prompts to name your bot and choose a username for it (e.g., MyGroupSpamBot)."
echo_info "4. BotFather will give you an API token. Copy this token carefully."
echo_info "   It will look something like: 1234567890:AAHxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
read -r -p "Enter your Telegram Bot Token: " TELEGRAM_TOKEN
while [[ -z "$TELEGRAM_TOKEN" ]]; do
    echo_error "Telegram Bot Token cannot be empty."
    read -r -p "Enter your Telegram Bot Token: " TELEGRAM_TOKEN
done

# Get Telegram Group ID/Name
echo_info "\n--- Telegram Group ID or Name ---"
echo_info "Provide the name of your public group or the ID of your private group."
echo_info "- For PUBLIC groups: You can use the group's username (e.g., my_public_group_name)."
echo_info "- For PRIVATE groups: You MUST use the group's ID. It's a negative number (e.g., -100123456789)."
echo_info "  How to get a private group ID:"
echo_info "  1. Add a bot like \"@RawDataBot\", \"@myidbot\", or \"@userinfobot\" to your group temporarily."
echo_info "  2. The bot will usually send a message containing the chat ID. Look for the 'id' field."
echo_info "  3. Alternatively, forward a message from your private group to one of these bots in a private chat with it."
echo_info "  4. Once you have the ID, you can remove the helper bot from your group."
read -r -p "Enter your Telegram Group Name or ID (e.g., mygroup or -100123456789): " TELEGRAM_GROUP
while [[ -z "$TELEGRAM_GROUP" ]]; do
    echo_error "Telegram Group Name/ID cannot be empty."
    read -r -p "Enter your Telegram Group Name or ID: " TELEGRAM_GROUP
done

# Get DigitalOcean Region
echo_info "Fetching available DigitalOcean regions..."
doctl compute region list --no-header --format "Slug,Name"
echo_info "Common regions: nyc1, nyc3, sfo2, sfo3, lon1, fra1, ams3, sgp1, tor1, blr1"
read -r -p "Enter the DigitalOcean region for your Droplet (e.g., nyc3): " DROPLET_REGION
while [[ -z "$DROPLET_REGION" ]]; do
    echo_error "DigitalOcean region cannot be empty."
    read -r -p "Enter the DigitalOcean region: " DROPLET_REGION
done

# Get Droplet Name
DEFAULT_DROPLET_NAME="${DEFAULT_DROPLET_NAME_PREFIX}-$(date +%s)"
read -r -p "Enter a name for your Droplet (default: ${DEFAULT_DROPLET_NAME}): " DROPLET_NAME
DROPLET_NAME=${DROPLET_NAME:-$DEFAULT_DROPLET_NAME}

# Get SSH Key
echo_info "Fetching your SSH keys from DigitalOcean..."
doctl compute ssh-key list --no-header --format "ID,Name,FingerPrint"
echo_info "If you don't see your key, make sure you've added it to your DigitalOcean account: https://docs.digitalocean.com/products/droplets/how-to/add-ssh-keys/"
read -r -p "Enter the ID or Fingerprint of the SSH key to use for the Droplet: " SSH_KEY_IDENTIFIER
while [[ -z "$SSH_KEY_IDENTIFIER" ]]; do
    echo_error "SSH Key ID/Fingerprint cannot be empty."
    read -r -p "Enter the SSH Key ID or Fingerprint: " SSH_KEY_IDENTIFIER
done

# Confirm settings
echo_info "\n--- Deployment Summary ---"
echo_info "Telegram Bot Token: **** (hidden for security)"
echo_info "Telegram Group:     $TELEGRAM_GROUP"
echo_info "DO Region:          $DROPLET_REGION"
echo_info "DO Droplet Name:    $DROPLET_NAME"
echo_info "DO Droplet Size:    $DEFAULT_DROPLET_SIZE"
echo_info "DO Droplet Image:   $DEFAULT_DROPLET_IMAGE"
echo_info "DO SSH Key:         $SSH_KEY_IDENTIFIER"
echo_info "TG-Spam Install Path: $TG_SPAM_INSTALL_PATH (on the Droplet)"

read -r -p "Proceed with deployment? (yes/no): " CONFIRMATION
if [[ "${CONFIRMATION,,}" != "yes" ]]; then
    echo_info "Deployment cancelled by user."
    exit 0
fi 

# 2. Create Droplet
echo_info "\nCreating DigitalOcean Droplet '$DROPLET_NAME'... This may take a few minutes."
doctl compute droplet create "$DROPLET_NAME" \
    --region "$DROPLET_REGION" \
    --size "$DEFAULT_DROPLET_SIZE" \
    --image "$DEFAULT_DROPLET_IMAGE" \
    --ssh-keys "$SSH_KEY_IDENTIFIER" \
    --user-data "#!/bin/bash
mkdir -p ${TG_SPAM_INSTALL_PATH}" \
    --wait \
    --format "ID,Name,PublicIPv4,Status"

if [ $? -ne 0 ]; then
    echo_error "Failed to create Droplet. Please check the output above for errors from 'doctl'."
    exit 1
fi

DROPLET_ID=$(doctl compute droplet list --name "^${DROPLET_NAME}$" --no-header --format ID)
DROPLET_IP=$(doctl compute droplet get "$DROPLET_ID" --no-header --format PublicIPv4)

echo_success "Droplet '$DROPLET_NAME' created successfully! IP Address: $DROPLET_IP"

echo_info "Waiting for Droplet to be fully ready and SSH to be available..."
START_TIME=$(date +%s)
TIMEOUT=300 # Timeout after 5 minutes
while true; do
    if ssh -o BatchMode=yes -o StrictHostKeyChecking=no -o ConnectTimeout=5 "root@${DROPLET_IP}" exit 2>/dev/null; then
        echo_success "SSH is now available on the Droplet."
        break
    fi
    CURRENT_TIME=$(date +%s)
    ELAPSED_TIME=$((CURRENT_TIME - START_TIME))
    if [ $ELAPSED_TIME -ge $TIMEOUT ]; then
        echo_error "Timeout waiting for SSH to become available. Please check the Droplet's status."
        exit 1
    fi
    sleep 5
done
# 3. Configure TG-Spam on the Droplet
echo_info "Configuring TG-Spam on the Droplet..."

# Create docker-compose.yml on the Droplet
cat << EOF | ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null "root@${DROPLET_IP}" "cat > ${TG_SPAM_INSTALL_PATH}/docker-compose.yml"
version: '3.8'
services:
  tgspam:
    image: umputun/tg-spam:latest
    container_name: tg-spam
    restart: always
    environment:
      - TELEGRAM_TOKEN=${TELEGRAM_TOKEN}
      - TELEGRAM_GROUP=${TELEGRAM_GROUP}
      # Optional: Add any other TG-Spam environment variables here.
      # Refer to the tg-spam README.md for a full list of available options.
      # Examples:
      # - OPENAI_TOKEN=your_openai_api_key_here
      # - OPENAI_MODEL=gpt-4
      # - MAX_EMOJI=5
      # - MESSAGE_SPAM="Spam detected and removed!"
      # - MIN_PROBABILITY=60 # Likelihood percentage (0-100) for a message to be considered spam by Bayes classifier
      # - CAS_API="" # To disable Combot Anti-Spam (CAS) integration
    volumes:
      - ${TG_SPAM_INSTALL_PATH}/data:/app/data # Persistent storage for SQLite DB, samples, etc.
    logging:
      driver: "json-file"
      options:
        max-size: "10m"
        max-file: "3"
EOF

if [ $? -ne 0 ]; then
    echo_error "Failed to create docker-compose.yml on the Droplet."
    echo_info "You might need to manually create it at ${TG_SPAM_INSTALL_PATH}/docker-compose.yml on the Droplet."
    exit 1
fi
echo_success "Created docker-compose.yml on the Droplet."

# Pull the latest image and start the container
echo_info "Pulling the latest TG-Spam Docker image..."
ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null "root@${DROPLET_IP}" "docker pull umputun/tg-spam:latest"
if [ $? -ne 0 ]; then
    echo_error "Failed to pull TG-Spam image on the Droplet."
    exit 1
fi

echo_info "Starting TG-Spam container using Docker Compose..."
ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null "root@${DROPLET_IP}" "docker compose -f ${TG_SPAM_INSTALL_PATH}/docker-compose.yml up -d"
if [ $? -ne 0 ]; then
    echo_error "Failed to start TG-Spam container on the Droplet."
    echo_info "You can try starting it manually: ssh root@${DROPLET_IP} \"docker compose -f ${TG_SPAM_INSTALL_PATH}/docker-compose.yml up -d\""
    exit 1
fi

# 4. Final Instructions
echo_success "TG-Spam deployment to DigitalOcean is complete!"
echo_info   "Your bot should now be running on Droplet '$DROPLET_NAME' ($DROPLET_IP)."
echo_info   "It might take a minute or two for the bot to fully initialize and connect to Telegram."

echo_info   "\n--- Important Information ---"
echo_info   "Droplet IP Address: $DROPLET_IP"
echo_info   "SSH Access: ssh root@$DROPLET_IP"
echo_info   "TG-Spam Logs: ssh root@$DROPLET_IP \"docker logs tg-spam -f\""
echo_info   "TG-Spam Config: ${TG_SPAM_INSTALL_PATH}/docker-compose.yml on the Droplet"

echo_info   "\nTo manage your bot:"
echo_info   "  Stop:    ssh root@$DROPLET_IP \"docker compose -f ${TG_SPAM_INSTALL_PATH}/docker-compose.yml down\""
echo_info   "  Start:   ssh root@$DROPLET_IP \"docker compose -f ${TG_SPAM_INSTALL_PATH}/docker-compose.yml up -d\""
echo_info   "  Restart: ssh root@$DROPLET_IP \"docker compose -f ${TG_SPAM_INSTALL_PATH}/docker-compose.yml restart\""

echo_info   "\nRemember to add your bot as an admin to your Telegram group: $TELEGRAM_GROUP"

echo_info "\nIf you encounter any issues, check the logs and ensure your Telegram Token and Group ID are correct."

exit 0

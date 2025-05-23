# DigitalOcean One-Click Deployment for TG-Spam

This script automates the deployment of the TG-Spam bot to a DigitalOcean Droplet.

## Prerequisites

1.  **DigitalOcean Account:** You need an active DigitalOcean account.
2.  **`doctl` CLI:** Install and configure the DigitalOcean command-line interface (`doctl`). You can find installation instructions [here](https://docs.digitalocean.com/reference/doctl/how-to/install/). Make sure you have authenticated `doctl` with your DigitalOcean account (`doctl auth init`).
3.  **SSH Key:** You should have an SSH key added to your DigitalOcean account. The script will use this to access the newly created Droplet. You can find instructions on how to add SSH keys [here](https://docs.digitalocean.com/products/droplets/how-to/add-ssh-keys/).
4.  **Telegram Bot Token:** You need a Telegram Bot Token. If you don't have one, talk to @BotFather on Telegram to create a new bot and get its token.
5.  **Telegram Group ID/Name:** The name or ID of the Telegram group where the bot will operate. For private groups, you'll need the group ID (e.g., `-123456789`). You can get this from bots like @myidbot.

## How to Use

1.  **Clone the repository (if you haven't already):**
    ```bash
    git clone https://github.com/umputun/tg-spam.git
    cd tg-spam
    ```

2.  **Navigate to the deployment script directory:**
    ```bash
    cd deploy/digitalocean
    ```

3.  **Make the script executable:**
    ```bash
    chmod +x deploy_do.sh
    ```

4.  **Run the script:**
    ```bash
    ./deploy_do.sh
    ```

5.  **Follow the prompts:** The script will ask you for:
    *   Your Telegram Bot Token.
    *   Your Telegram Group ID/Name.
    *   The DigitalOcean region where you want to deploy the Droplet (e.g., `nyc3`, `lon1`).
    *   The name you want to give to your Droplet.
    *   The SSH key ID or fingerprint to use for the Droplet.

The script will then:
*   Create a new DigitalOcean Droplet.
*   Install Docker and Docker Compose on the Droplet.
*   Create a `docker-compose.yml` file for TG-Spam.
*   Start the TG-Spam bot using Docker Compose.

Once the script finishes, your TG-Spam bot should be running on the newly created Droplet. The script will output the IP address of the Droplet.

## Configuration Options (Inside `deploy_do.sh`)

You can modify the following variables at the beginning of the `deploy_do.sh` script if needed:

*   `DROPLET_SIZE`: The size of the Droplet (default: `s-1vcpu-1gb`).
*   `DROPLET_IMAGE`: The OS image for the Droplet (default: `docker-20-04`). This image comes with Docker pre-installed.

## Post-Deployment

*   **Accessing the Droplet:** You can SSH into your Droplet using `ssh root@<DROPLET_IP_ADDRESS>`.
*   **Managing the Bot:**
    *   To see logs: `ssh root@<DROPLET_IP_ADDRESS> "docker-compose -f /opt/tg-spam/docker-compose.yml logs -f"`
    *   To stop the bot: `ssh root@<DROPLET_IP_ADDRESS> "docker-compose -f /opt/tg-spam/docker-compose.yml down"`
    *   To start the bot: `ssh root@<DROPLET_IP_ADDRESS> "docker-compose -f /opt/tg-spam/docker-compose.yml up -d"`
*   **Updating the Bot:** To update to the latest version of TG-Spam, SSH into the Droplet and run:
    ```bash
    cd /opt/tg-spam
    docker-compose pull tgspam
    docker-compose up -d --remove-orphans
    ```

## Troubleshooting

*   Ensure `doctl` is correctly configured and authenticated.
*   Verify your SSH key is correctly added to your DigitalOcean account and that you provide the correct ID or fingerprint to the script.
*   Check the script's output for any error messages from `doctl` or during the Droplet setup.

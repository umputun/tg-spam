# Easy Installation Guide for TG-Spam

This guide is designed for users who want to set up TG-Spam but don't have technical experience. We'll walk through the process step by step in plain language.

## What You'll Need Before Starting

1. A computer running Windows, Mac, or Linux
2. A Telegram account
3. Admin rights in the Telegram group you want to protect
4. About 15-20 minutes of your time

## Step 1: Getting Your Bot Token from Telegram

First, we need to create a bot with Telegram:

1. Open Telegram and search for "@BotFather"
2. Click "Start" to begin chatting with BotFather
3. Type `/newbot` and send it
4. BotFather will ask for a name for your bot. Type any name you like (e.g., "My Group's Spam Protector")
5. Next, create a username for your bot. It must end in "bot" (e.g., "mygroupspambot" or "my_group_spam_bot")
6. BotFather will give you a token - it looks like a long string of numbers and letters. **Save this token somewhere safe - you'll need it later!**

Remember: Never share your bot token with anyone - it's like a password for your bot!

## Step 2: Setting Up Docker (The Program That Runs TG-Spam)

*Please note: using an instance, droplet, virtual machine, VPS, or whatever it is called within the provider of your choice with preinstalled Docker and Docker Compose is usually a better and simpler choice. In this case, you can skip this step. To make sure you have Docker installed, run `docker --version` and `docker compose --version` in your terminal.*

**See the [official Docker Desktop documentation](https://docs.docker.com/desktop/) for more detailed instructions.**

Docker Desktop is a program that helps run TG-Spam on your computer. Here's how to install it:

### For Windows:
1. Go to [Docker Desktop for Windows](https://www.docker.com/products/docker-desktop/)
2. Click the "Download for Windows" button
3. Once downloaded, double-click the installer
4. Follow the installation wizard, keeping all default settings
5. Restart your computer when asked

### For Mac:
1. Go to [Docker Desktop for Mac](https://www.docker.com/products/docker-desktop/)
2. Click the "Download for Mac" button
3. Once downloaded, drag Docker to your Applications folder
4. Double-click Docker in Applications to start it
5. Follow any prompts that appear

### For Linux:

**See the [official docs](https://docs.docker.com/engine/install/) for more detailed instructions.**

1. Open Terminal
2. Copy and paste these commands one at a time:

For Ubuntu/Debian:
```bash
# Update your system
sudo apt update
sudo apt upgrade

# Install required packages
sudo apt install curl

# Add Docker's official GPG key
curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo gpg --dearmor -o /usr/share/keyrings/docker-archive-keyring.gpg

# Add Docker repository
echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/docker-archive-keyring.gpg] https://download.docker.com/linux/ubuntu $(lsb_release -cs) stable" | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null

# Install Docker
sudo apt update
sudo apt install docker-ce docker-ce-cli containerd.io docker-compose-plugin

# Add your user to docker group (so you don't need sudo for docker commands)
sudo usermod -aG docker $USER

# Start Docker
sudo systemctl start docker
sudo systemctl enable docker
```

For Fedora:
```bash
# Install required packages
sudo dnf -y install dnf-plugins-core

# Add Docker repository
sudo dnf config-manager --add-repo https://download.docker.com/linux/fedora/docker-ce.repo

# Install Docker
sudo dnf install docker-ce docker-ce-cli containerd.io docker-compose-plugin

# Add your user to docker group
sudo usermod -aG docker $USER

# Start Docker
sudo systemctl start docker
sudo systemctl enable docker
```

3. Log out and log back in for the group changes to take effect
4. Test Docker by running: `docker --version`


## Step 3: Creating Your Configuration File

1. Create a new folder on your computer called "tg-spam"
2. Inside this folder, create a new text file named `docker-compose.yml`
3. Copy and paste this template into the file:

```yaml
services:
  tg-spam:
    image: umputun/tg-spam:latest
    restart: always
    environment:
      - TELEGRAM_TOKEN=YOUR_BOT_TOKEN_HERE
      - TELEGRAM_GROUP=YOUR_GROUP_NAME_HERE
    volumes:
      - ./data:/srv/data
```

4. Replace `YOUR_BOT_TOKEN_HERE` with the token you got from BotFather
5. Replace `YOUR_GROUP_NAME_HERE` with your Telegram group's username (without the @ symbol)

## Step 4: Starting TG-Spam

1. Open Terminal (Mac and Linux) or Command Prompt (Windows)
2. Type `cd ` (with a space after cd) and drag your tg-spam folder into the window
3. Press Enter
4. Type this command and press Enter:
```
docker-compose up -d
```

## Step 5: Adding the Bot to Your Group

1. Go to your Telegram group
2. Click the group name at the top
3. Click "Add members" or "Add"
4. Search for your bot using the username you created
5. Add the bot
6. Make the bot an admin:
    - Click the group name again
    - Click "Administrators" or "Manage group"
    - Click "Add Admin"
    - Find your bot and select it
    - Enable all permissions except "Anonymous"
    - Click "Done" or "Save"

## That's it! Your bot is now running and protecting your group from spam.

## Common Questions

**Q: How do I know if it's working?**
A: The bot will automatically start monitoring messages. Try sending a test message in your group - the bot should be active and monitoring.

**Q: How do I stop the bot?**
A: In Terminal/Command Prompt, go to your tg-spam folder and type: `docker-compose down`

**Q: How do I update the bot?**
A: In Terminal/Command Prompt, go to your tg-spam folder and type:
```
docker-compose pull
docker-compose up -d
```

**Q: Something's not working. What should I check?**
1. Make sure Docker Desktop is running
2. Verify your bot token is correct
3. Confirm the bot has admin rights in your group
4. Check that your group name is entered correctly in the configuration


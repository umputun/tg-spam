#!/bin/bash

# This script will download all messages from the CAS API and concatenate them into one file.
# The file will be saved in the same directory as this script and will be named 'messages.txt'.
# The script requires jq to be installed (https://stedolan.github.io/jq/).
# Loading all messages from CAS will take a long time, hours or even days, depending on the number of messages.
# The resulting file can be used as a generic spam sample file fot tg-spam bot.

DEST_FILE="messages.txt"
rm -fv "$DEST_FILE"
curl https://api.cas.chat/export.csv -o export.csv

counter=0

tail -n +2 export.csv | cut -d',' -f1 | while read -r user_id; do
    ((counter++))


    response=$(curl -s "https://api.cas.chat/check?user_id=$user_id")

    if [[ $(echo "$response" | jq -r '.ok') == "true" ]]; then
        echo "Processing user_id $user_id... ($counter)"
        concatenated_messages=$(echo "$response" | jq -r '[.result.messages[]] | join(" ")' | tr '\n' ' ')
        echo "$concatenated_messages" >> "$DEST_FILE"
    fi
done

rm export.csv

echo "Processing complete. Total user_ids processed: $counter, messages written to $DEST_FILE - $(wc -l "$DEST_FILE").

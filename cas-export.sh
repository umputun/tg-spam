#!/bin/bash

# Define the destination file for messages
DEST_FILE="messages.txt"
rm -fv "$DEST_FILE"
# Download the CSV file
curl https://api.cas.chat/export.csv -o export.csv

# Initialize a counter
counter=0

# Read each line (user_id) from the CSV
tail -n +2 export.csv | cut -d',' -f1 | while read -r user_id; do
    # Increment the counter
    ((counter++))


    # Call the API with the user_id
    response=$(curl -s "https://api.cas.chat/check?user_id=$user_id")

    # Check if 'ok' is true
    if [[ $(echo "$response" | jq -r '.ok') == "true" ]]; then
        # Display progress
        echo "Processing user_id $user_id... ($counter)"

        # Extract and concatenate messages into one string
        concatenated_messages=$(echo "$response" | jq -r '[.result.messages[]] | join(" ")' | tr '\n' ' ')

        # Append the concatenated string to the destination file
        echo "$concatenated_messages" >> "$DEST_FILE"
    fi
done

# Clean up
rm export.csv

# Final status message
echo "Processing complete. Total user_ids processed: $counter, messages written to $DEST_FILE - $(wc -l "$DEST_FILE").

# Lua Plugins for tg-spam

This directory contains examples of Lua plugins for tg-spam. These plugins demonstrate how to create custom spam checkers using Lua.

## How Lua Plugins Work

Lua plugins allow you to create custom spam detection logic without modifying the Go code. Each plugin is a Lua script that defines a `check` function that takes a request object and returns whether the message is spam, details, and an optional approval for the current message.

### Plugin Requirements

- Each plugin must be a `.lua` file
- Each plugin must define a `check` function
- The `check` function must return two values and may return a third:
  - A boolean indicating whether the message is spam (true) or not (false)
  - A string providing details about the check
  - An optional boolean approving the current message when the spam result is false

Missing, `nil`, and boolean `false` approval values have no effect. Only an exact boolean `true` is accepted. Other types are ignored and logged once per plugin and type. A plugin error never approves a message, and a plugin cannot report spam and approve it in the same result.

An effective approval clears soft spam results from the current `Detector.Check` call and produces one `lua-approve` result row. It can clear duplicate, stop-word, emoji, meta, Lua, CAS, multi-language, abnormal-spacing, similarity, and classifier results. It cannot clear short-message-flood or prohibited-language results because those checks return before Lua plugins run.

Cleared short messages do not enter ham history or count toward user graduation. Cleared normal-length messages follow the ordinary ham path and enter the bounded ham history. They also count toward configured user graduation unless the request is check-only. With the default `--first-messages-count=1`, one cleared normal-length message graduates the sender, so later messages skip content analysis under the existing graduation rules. Cleared messages can become context for later LLM checks when LLM history is enabled.

### Request Object Structure

Your Lua plugin receives a `request` object with the following fields:

- `msg`: The message text
- `user_id`: The user ID
- `user_name`: The username
- `first_name`: The user's first name
- `last_name`: The user's last name
- `is_premium`: Boolean indicating if the user has Telegram Premium
- `meta`: A table with metadata:
  - `images`: Number of images in the message
  - `links`: Number of links in the message
  - `mentions`: Number of mentions in the message
  - `has_video`: Boolean indicating if the message has a video
  - `has_audio`: Boolean indicating if the message has audio
  - `has_forward`: Boolean indicating if the message is forwarded
  - `has_keyboard`: Boolean indicating if the message has a keyboard
  - `has_giveaway`: Boolean indicating if the message is a giveaway
  - `has_contact`: Boolean indicating if the message has a shared contact
  - `has_document`: Boolean indicating if the message has a document (telegram also sets this for GIFs)
  - `message_id`: The Telegram message ID

### Helper Functions

The following helper functions are available:

#### String Manipulation
- `count_substring(text, substr)`: Counts occurrences of a substring
- `match_regex(text, pattern)`: Checks if text matches a regex pattern
- `contains_any(text, [substrings])`: Checks if text contains any of the given substrings
- `to_lower(text)`: Converts text to lowercase
- `to_upper(text)`: Converts text to uppercase
- `trim(text)`: Removes whitespace from both ends of text
- `split(text, separator)`: Splits text by separator
- `join(separator, [strings])`: Joins strings with a separator
- `starts_with(text, prefix)`: Checks if text starts with prefix
- `ends_with(text, suffix)`: Checks if text ends with suffix

#### HTTP and JSON Processing
- `http_request(url, [method="GET"], [headers={}], [body=""], [timeout=5])`: Makes an HTTP request
  - Returns: `response_body, status_code, error`
  - Default timeout is 5 seconds, but can be customized
- `json_encode(value)`: Converts a Lua table to a JSON string
  - Returns: `json_string, error`
- `json_decode(json_string)`: Parses a JSON string into a Lua table
  - Returns: `lua_table, error`
- `url_encode(string)`: Encodes a string for safe use in URLs

## Example Plugins

This directory contains the following example plugins:

1. `repeat_chars.lua`: Detects messages with excessive repeated characters
2. `domain_blacklist.lua`: Checks messages for blacklisted domains
3. `message_pattern.lua`: Checks for common spam message patterns
4. `api_check.lua`: Demonstrates using HTTP requests to check messages with external APIs
5. `arabic_script_detector.lua`: Identifies users with Arabic script names in non-Arabic chats

## Using Lua Plugins

To enable Lua plugins in tg-spam:

1. Place your Lua scripts in a directory
2. Configure tg-spam to use Lua plugins by setting:
   ```
   --lua-plugins.enabled
   --lua-plugins.plugins-dir=/path/to/plugins
   ```
3. Optionally, specify which plugins to enable:
   ```
   --lua-plugins.enabled-plugins=plugin1,plugin2
   ```
   If no plugins are specified, all plugins in the directory will be loaded.

## Writing Your Own Plugins

Here's a simple example of a custom Lua plugin:

```lua
-- example.lua
-- Detects messages with too many exclamation marks

-- Configuration
local config = {
    max_exclamations = 3,
    approved_user_ids = {
        -- Add Telegram user IDs that may approve the current message:
        -- ["123456789"] = true,
    }
}

-- Main check function
function check(request)
    local count = count_substring(request.msg, "!")
    
    if count > config.max_exclamations then
        return true, string.format("too many exclamation marks: %d", count)
    end

    local approve = config.approved_user_ids[request.user_id] == true
    return false, "normal number of exclamation marks", approve
end
```

Save this as a `.lua` file in your plugins directory and tg-spam will load it automatically if Lua plugins are enabled.

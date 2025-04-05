-- repeat_chars.lua
-- Detects messages with excessive repeated characters (common in spam)

-- Configuration
local config = {
    max_repeats = 5,  -- Max consecutive identical characters allowed
    min_msg_length = 10,  -- Don't check messages shorter than this
}

-- Main check function - must be named "check"
-- @param request - Table with message details
-- @return boolean - true if spam, false if not spam
-- @return string - Details message
function check(request)
    local msg = request.msg
    
    -- Skip very short messages
    if #msg < config.min_msg_length then
        return false, "message too short to check"
    end
    
    -- Convert to lowercase for better matching
    msg = to_lower(msg)
    
    -- Look for repeated character sequences
    local current_char = ""
    local repeat_count = 1
    local max_repeats = 1
    
    for i = 1, #msg do
        local char = string.sub(msg, i, i)
        
        if char == current_char then
            repeat_count = repeat_count + 1
            if repeat_count > max_repeats then
                max_repeats = repeat_count
            end
        else
            current_char = char
            repeat_count = 1
        end
        
        -- Early exit if we already found excessive repeats
        if max_repeats > config.max_repeats then
            return true, string.format("excessive repeated characters (%d/%d)", 
                max_repeats, config.max_repeats)
        end
    end
    
    return false, string.format("normal character repetition (%d/%d)", 
        max_repeats, config.max_repeats)
end
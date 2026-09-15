-- arabic_script_detector.lua
-- Detects users with Arabic script names in chats that don't use Arabic

-- Function to check if string contains Arabic script characters
function contains_arabic(str)
    -- Check for Arabic unicode range (0600-06FF for Arabic, 0750-077F for Arabic Supplement)
    -- This will match Arabic characters while allowing spaces, numbers, and punctuation
    local has_arabic = match_regex(str, "[\\x{0600}-\\x{06FF}\\x{0750}-\\x{077F}]")
    return has_arabic
end

-- Main check function - must be named "check"
function check(request)
    local user_name = request.user_name
    
    -- Skip check if username is empty
    if user_name == "" then
        return false, "no username to check"
    end
    
    -- Check if the username contains Arabic characters
    if contains_arabic(user_name) then
        -- In a real implementation, you might:
        -- 1. Check against chat history to determine if Arabic is commonly used
        -- 2. Consider additional factors like account age, first message, etc.
        -- 3. Only mark as potential spam if this is unusual for the specific chat
        
        return true, "username contains Arabic script which may be unusual for this chat"
    end
    
    return false, "username does not contain Arabic script"
end
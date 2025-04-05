-- message_pattern.lua
-- Checks message patterns common in spam

-- Configuration
local patterns = {
    {
        pattern = "earn.*money.*online",
        description = "earn money online scam"
    },
    {
        pattern = "make.*money.*fast",
        description = "make money fast scam"
    },
    {
        pattern = "join.*channel.*t%.me",
        description = "channel promotion spam"
    },
    {
        pattern = "invest.*[0-9]+%%.*profit",
        description = "investment scam"
    },
    {
        pattern = "dating.*site",
        description = "dating site spam"
    },
    {
        pattern = "join.*community.*t%.me",
        description = "community promotion spam"
    },
    {
        pattern = "free.*bitcoin",
        description = "crypto scam"
    },
    {
        pattern = "promotion.*code",
        description = "promotion code spam"
    },
    {
        pattern = "work.*from.*home.*[0-9]+.*day",
        description = "work from home scam"
    }
}

-- Main check function - must be named "check"
function check(request)
    local msg = to_lower(request.msg)
    
    -- Check for each pattern
    for _, p in ipairs(patterns) do
        local matched = match_regex(msg, p.pattern)
        if matched then
            return true, p.description
        end
    end
    
    return false, "no spam patterns detected"
end
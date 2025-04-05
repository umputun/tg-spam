-- simple_test.lua
-- A simple test plugin that detects specific keywords in messages

-- Configuration
local spam_keywords = {
    "crypto", "investment", "urgently", "bitcoin", "earn money",
    "make money", "opportunity", "financial freedom", "passive income"
}

function check(request)
    local msg = to_lower(request.msg)
    
    for _, keyword in ipairs(spam_keywords) do
        if count_substring(msg, keyword) > 0 then
            return true, "detected spam keyword: " .. keyword
        end
    end
    
    return false, "no spam keywords detected"
end
-- domain_blacklist.lua
-- Checks messages for blacklisted domains

-- Configuration - list of suspicious TLDs and domain patterns
local blacklisted_tlds = {
    ".xyz", ".top", ".online", ".site", ".club", ".gq", ".tk", ".ml", ".ga", ".cf"
}

local blacklisted_domains = {
    "bit.ly", "tinyurl.com", "goo.gl", "t.co", 
    "money", "casino", "bonus", "crypto", "bitcoin",
    "prize", "win", "lucky", "investment"
}

-- Helper function to extract domains from text
local function extract_domains(text)
    local domains = {}
    local pattern = "https?://([%w%-%.]+)/?[%w%-%.?=&%%+#]*"
    
    for domain in string.gmatch(text, pattern) do
        table.insert(domains, domain)
    end
    
    return domains
end

-- Main check function - must be named "check"
function check(request)
    local msg = request.msg
    
    -- Extract all domains from the message
    local domains = extract_domains(msg)
    
    -- If no domains found, it's not spam by this check
    if #domains == 0 then
        return false, "no domains found"
    end
    
    -- Check each domain against blacklists
    for _, domain in ipairs(domains) do
        -- Check for blacklisted TLDs
        for _, tld in ipairs(blacklisted_tlds) do
            if ends_with(domain, tld) then
                return true, "blacklisted TLD: " .. tld
            end
        end
        
        -- Check for blacklisted domain patterns
        for _, pattern in ipairs(blacklisted_domains) do
            if string.find(domain, pattern) then
                return true, "suspicious domain pattern: " .. pattern
            end
        end
    end
    
    return false, "no blacklisted domains found"
end
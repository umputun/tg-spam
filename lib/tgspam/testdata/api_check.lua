-- api_check.lua
-- Example Lua plugin that uses HTTP requests and JSON processing to check messages
-- against an external API for spam detection

function check(req)
  -- Normalize the message
  local msg = to_lower(req.msg)
  
  -- Check for common spam patterns before making API requests
  if count_substring(msg, "crypto") > 0 or count_substring(msg, "investment") > 0 then
    -- We can detect this without an API call
    return true, "local detection: spam keywords found"
  end
  
  -- Prepare data for the API request
  local request_data = {
    message = req.msg,
    user_id = req.user_id,
    user_name = req.user_name
  }
  
  -- Create headers
  local headers = {
    ["Content-Type"] = "application/json",
    ["Accept"] = "application/json"
  }
  
  -- Convert data to JSON
  local json_data, json_err = json_encode(request_data)
  if json_err then
    return false, "json encoding error: " .. json_err
  end
  
  -- This URL is just an example - replace with a real API endpoint in production
  -- Note: In a real implementation, this would be a configurable endpoint
  -- In this example, we'll check for the existence of a mock API and skip the request if not available
  local endpoint = "https://example.com/api/spam-check"
  
  -- Optional: check if API is configured or use a mock response for testing
  if req.msg == "__test_mode__" then
    -- For testing, return a mock response
    return false, "API check skipped in test mode"
  end
  
  -- Make a POST request to the API with a short timeout (3 seconds)
  local response, status, err = http_request(endpoint, "POST", headers, json_data, 3)
  
  -- Handle request errors
  if err then
    -- If API request failed, log the error but allow the message
    -- In production, you might want to fall back to other spam detection methods
    return false, "API request failed: " .. err .. " (allowing message)"
  end
  
  -- Check for non-200 status codes
  if status ~= 200 then
    -- API is not responding correctly
    return false, "API returned status " .. status .. " (allowing message)"
  end
  
  -- Parse API response
  local result, parse_err = json_decode(response)
  if parse_err then
    -- Failed to parse response
    return false, "API response parse error: " .. parse_err .. " (allowing message)"
  end
  
  -- Process API result (the structure depends on your API)
  if result.is_spam then
    -- API detected spam
    return true, "API detection: " .. (result.reason or "unknown reason")
  end
  
  -- Message is not spam according to the API
  return false, "API verified: not spam"
end
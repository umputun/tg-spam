-- api_check.lua
-- Demonstrates using HTTP requests and JSON processing to check messages
-- against an external API for spam detection
--
-- This plugin shows how to:
-- 1. Perform local pre-filtering to avoid unnecessary API calls 
-- 2. Create and format JSON data
-- 3. Set proper headers for API requests
-- 4. Make HTTP POST requests with timeout control
-- 5. Handle various error conditions (network errors, non-200 responses)
-- 6. Parse and process JSON responses
-- 7. Make decisions based on confidence scores

function check(req)
  -- Normalize the message
  local msg = to_lower(req.msg)
  
  -- Check for common spam patterns first (optional pre-filtering)
  if count_substring(msg, "crypto") > 0 and count_substring(msg, "invest") > 0 then
    -- We can detect this without an API call
    return true, "local detection: common spam pattern detected"
  end
  
  -- Prepare data for the API request
  local request_data = {
    message = req.msg,
    user_id = req.user_id,
    chat_id = req.chat_id,
    user_name = req.user_name
  }
  
  -- Create headers for JSON request
  local headers = {
    ["Content-Type"] = "application/json",
    ["Accept"] = "application/json"
  }
  
  -- Convert data to JSON
  local json_data, json_err = json_encode(request_data)
  if json_err then
    return false, "JSON encoding error: " .. json_err
  end
  
  -- This URL is just an example - replace with a real API endpoint
  -- In a production environment, you might want to:
  -- 1. Use a configuration system to set this URL
  -- 2. Add authentication tokens to the request
  -- 3. Implement rate limiting to avoid API overuse
  local endpoint = "https://api.example.com/spam-check"
  
  -- For testing purposes only - if you want to test without hitting a real API
  if req.msg == "__test_mode__" then
    return false, "API check skipped in test mode"
  end
  
  -- Make HTTP request with a 3 second timeout
  local response, status, err = http_request(endpoint, "POST", headers, json_data, 3)
  
  -- Handle request errors (network issues, timeouts, etc.)
  if err then
    -- Log the error but allow the message (fail open)
    -- In production, you might want to implement fallback mechanisms:
    -- 1. Try an alternative API endpoint
    -- 2. Fall back to local spam detection
    -- 3. Record failures for monitoring
    return false, "API request failed: " .. err .. " (allowing message)"
  end
  
  -- Check for unsuccessful status codes
  if status ~= 200 then
    return false, "API returned status " .. status .. " (allowing message)"
  end
  
  -- Parse API response - convert JSON string to Lua table
  local result, parse_err = json_decode(response)
  if parse_err then
    -- Handle JSON parsing errors gracefully
    return false, "Failed to parse API response: " .. parse_err .. " (allowing message)"
  end
  
  -- Process API result (example response format - adjust according to your API)
  -- This assumes a response structure like:
  -- {
  --   "is_spam": true/false,
  --   "confidence": 0.95, (0-1 scale)
  --   "reason": "Detected cryptocurrency scam patterns"
  -- }
  if result.is_spam == true then
    local confidence = result.confidence or 0
    local reason = result.reason or "unspecified reason"
    
    -- Only block if high confidence (threshold can be configured)
    if confidence > 0.8 then
      -- High confidence spam - block the message
      return true, "API detection (confidence: " .. confidence .. "): " .. reason
    else
      -- Low confidence - warn but allow the message
      -- This helps reduce false positives
      return false, "Low confidence spam detection: " .. reason
    end
  end
  
  -- Message is not spam according to the API
  return false, "API verified: not spam"
end
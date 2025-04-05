package main

import (
	"fmt"
	"log"
	"path/filepath"
	"time"

	"github.com/umputun/tg-spam/lib/spamcheck"
	"github.com/umputun/tg-spam/lib/tgspam"
	"github.com/umputun/tg-spam/lib/tgspam/lua"
)

func main() {
	// Create a detector with Lua plugins enabled
	config := tgspam.Config{
		MinMsgLen:       10,    // Minimum message length to check
		MaxAllowedEmoji: 5,     // Maximum number of emojis allowed
		MultiLangWords:  3,     // Check for multiple languages in words
	}

	// Enable Lua plugins
	config.LuaPlugins.Enabled = true
	config.LuaPlugins.PluginsDir = "."  // Use current directory for plugins
	
	// Create the detector
	detector := tgspam.NewDetector(config)
	
	// Create and add Lua plugin engine
	luaEngine := lua.NewLuaChecker()
	err := detector.WithLuaEngine(luaEngine)
	if err != nil {
		log.Fatalf("Failed to initialize Lua plugin engine: %v", err)
	}
	defer luaEngine.Close()
	
	// Print loaded plugins
	pluginDir, _ := filepath.Abs(config.LuaPlugins.PluginsDir)
	fmt.Printf("Loaded Lua plugins from: %s\n", pluginDir)
	
	// Test some messages with the detector
	testMessages := []spamcheck.Request{
		{
			Msg:      "This is a normal message",
			UserID:   "user1",
			UserName: "john_doe",
		},
		{
			Msg:      "HEEEEELLLLLOOOOO!!!! This message has tooooo manyyyy repeated characters!!!!",
			UserID:   "user2",
			UserName: "spam_user",
		},
		{
			Msg:      "Check out this investment opportunity at crypto-investment.xyz - earn 50% profit!",
			UserID:   "user3",
			UserName: "crypto_fan",
		},
		{
			Msg:      "Free Bitcoin! Join our community and earn money fast with this amazing opportunity!",
			UserID:   "user4",
			UserName: "bitcoinguru",
		},
	}
	
	// Check each message
	for i, msg := range testMessages {
		fmt.Printf("\n=== Testing Message %d ===\n", i+1)
		fmt.Printf("Message: %s\n", msg.Msg)
		
		isSpam, responses := detector.Check(msg)
		
		fmt.Printf("Spam detected: %v\n", isSpam)
		fmt.Println("Check results:")
		for _, resp := range responses {
			if resp.Spam {
				fmt.Printf("  [SPAM] %s: %s\n", resp.Name, resp.Details)
			} else {
				fmt.Printf("  [HAM]  %s: %s\n", resp.Name, resp.Details)
			}
		}
		
		time.Sleep(100 * time.Millisecond) // Short delay between checks
	}
}
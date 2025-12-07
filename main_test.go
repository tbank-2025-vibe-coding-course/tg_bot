package main

import (
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Mocking the bot API for unit tests is complex because the struct fields are private/hard to interface.
// However, we can test the Logic Helper functions and Storage persistence.

func TestStoragePersistence(t *testing.T) {
	tmpFile := "test_storage.json"
	storage := NewStorage(tmpFile)

	userID := int64(12345)
	session := storage.GetOrCreateSession(userID)
	session.UserData["age"] = "30"
	session.State = StateTypingReply

	storage.Save()

	// Create new storage instance loading from the same file
	storage2 := NewStorage(tmpFile)
	loadedSession := storage2.GetSession(userID)

	if loadedSession == nil {
		t.Fatal("Failed to load session from disk")
	}

	if loadedSession.UserData["age"] != "30" {
		t.Errorf("Expected age '30', got '%s'", loadedSession.UserData["age"])
	}

	if loadedSession.State != StateTypingReply {
		t.Errorf("Expected state %d, got %d", StateTypingReply, loadedSession.State)
	}
}

func TestFactsToString(t *testing.T) {
	data := map[string]string{
		"age":   "25",
		"color": "blue",
	}
	result := factsToString(data)
	if result == "" {
		t.Error("Result should not be empty")
	}
}

// A simple mock for Update
func makeMessageUpdate(text string) tgbotapi.Update {
	return tgbotapi.Update{
		Message: &tgbotapi.Message{
			Text: text,
			From: &tgbotapi.User{
				ID:       1,
				UserName: "TestUser",
			},
			Chat: &tgbotapi.Chat{
				ID: 1,
			},
		},
	}
}

// Note: Testing ProcessUpdate fully requires mocking the tgbotapi.BotAPI which performs network calls.
// In a real generic architecture, we would wrap BotAPI in an interface (Sender).
// For this strict single-file task, we focused on testing the State/Storage logic.

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"sync"
	"syscall"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// --- Constants & Enums ---

const (
	StateChoosing = iota
	StateTypingReply
	StateTypingChoice
)

const (
	StorageFile = "/data/conversationbot.json" // Path for Docker volume
)

// --- Structures ---

// UserSession holds the state and data for a specific user.
type UserSession struct {
	State       int               `json:"state"`
	CurrentKey  string            `json:"current_key,omitempty"` // Analogous to context.user_data["choice"]
	UserData    map[string]string `json:"user_data"`
	LastUpdated int64             `json:"last_updated"`
}

// ThreadSafeStorage handles concurrent access to user sessions and file persistence.
type ThreadSafeStorage struct {
	sync.RWMutex
	Sessions map[int64]*UserSession `json:"sessions"`
	FilePath string
}

// --- Storage Logic ---

func NewStorage(filePath string) *ThreadSafeStorage {
	storage := &ThreadSafeStorage{
		Sessions: make(map[int64]*UserSession),
		FilePath: filePath,
	}
	storage.Load()
	return storage
}

func (s *ThreadSafeStorage) GetSession(userID int64) *UserSession {
	s.RLock()
	defer s.RUnlock()
	if session, exists := s.Sessions[userID]; exists {
		return session
	}
	return nil
}

func (s *ThreadSafeStorage) GetOrCreateSession(userID int64) *UserSession {
	s.Lock()
	defer s.Unlock()
	if _, exists := s.Sessions[userID]; !exists {
		s.Sessions[userID] = &UserSession{
			State:    StateChoosing,
			UserData: make(map[string]string),
		}
	}
	return s.Sessions[userID]
}

// Save dumps the in-memory store to a JSON file.
func (s *ThreadSafeStorage) Save() {
	s.RLock()
	defer s.RUnlock()

	data, err := json.MarshalIndent(s.Sessions, "", "  ")
	if err != nil {
		log.Printf("[ERROR] Failed to marshal storage: %v", err)
		return
	}

	// Simple write (in production, write to temp and rename is safer)
	err = os.WriteFile(s.FilePath, data, 0644)
	if err != nil {
		log.Printf("[ERROR] Failed to save storage to file: %v", err)
	} else {
		log.Println("[INFO] Storage saved successfully.")
	}
}

// Load reads the JSON file into memory.
func (s *ThreadSafeStorage) Load() {
	s.Lock()
	defer s.Unlock()

	data, err := os.ReadFile(s.FilePath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Println("[INFO] No existing storage file found. Starting fresh.")
			return
		}
		log.Printf("[ERROR] Failed to read storage file: %v", err)
		return
	}

	if len(data) == 0 {
		return
	}

	err = json.Unmarshal(data, &s.Sessions)
	if err != nil {
		log.Printf("[ERROR] Failed to unmarshal storage: %v", err)
		return
	}
	log.Printf("[INFO] Loaded %d sessions from disk.", len(s.Sessions))
}

// --- Keyboards ---

var mainKeyboard = tgbotapi.NewReplyKeyboard(
	tgbotapi.NewKeyboardButtonRow(
		tgbotapi.NewKeyboardButton("Age"),
		tgbotapi.NewKeyboardButton("Favourite colour"),
	),
	tgbotapi.NewKeyboardButtonRow(
		tgbotapi.NewKeyboardButton("Number of siblings"),
		tgbotapi.NewKeyboardButton("Something else..."),
	),
	tgbotapi.NewKeyboardButtonRow(
		tgbotapi.NewKeyboardButton("Done"),
	),
)

// --- Helper Functions ---

func factsToString(userData map[string]string) string {
	var facts []string
	for k, v := range userData {
		facts = append(facts, fmt.Sprintf("%s - %s", k, v))
	}
	return strings.Join(facts, "\n")
}

// --- Bot Logic Handlers ---

// handleStart initiates the conversation.
func handleStart(update *tgbotapi.Update, session *UserSession, bot *tgbotapi.BotAPI) {
	reply := "Hi! My name is Doctor Botter."
	if len(session.UserData) > 0 {
		keys := make([]string, 0, len(session.UserData))
		for k := range session.UserData {
			keys = append(keys, k)
		}
		reply += fmt.Sprintf(" You already told me your %s. Why don't you tell me something more about yourself? Or change anything I already know.", strings.Join(keys, ", "))
	} else {
		reply += " I will hold a more complex conversation with you. Why don't you tell me something about yourself?"
	}

	msg := tgbotapi.NewMessage(update.Message.Chat.ID, reply)
	msg.ReplyMarkup = mainKeyboard
	bot.Send(msg)
	session.State = StateChoosing
}

// handleRegularChoice handles predefined categories.
func handleRegularChoice(update *tgbotapi.Update, session *UserSession, bot *tgbotapi.BotAPI) {
	text := strings.ToLower(update.Message.Text)
	session.CurrentKey = text

	var replyText string
	if val, ok := session.UserData[text]; ok {
		replyText = fmt.Sprintf("Your %s? I already know the following about that: %s", text, val)
	} else {
		replyText = fmt.Sprintf("Your %s? Yes, I would love to hear about that!", text)
	}

	msg := tgbotapi.NewMessage(update.Message.Chat.ID, replyText)
	bot.Send(msg)
	session.State = StateTypingReply
}

// handleCustomChoice asks for a custom category name.
func handleCustomChoice(update *tgbotapi.Update, session *UserSession, bot *tgbotapi.BotAPI) {
	msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Alright, please send me the category first, for example \"Most impressive skill\"")
	bot.Send(msg)
	session.State = StateTypingChoice
}

// handleReceivedInformation saves the user input.
func handleReceivedInformation(update *tgbotapi.Update, session *UserSession, bot *tgbotapi.BotAPI) {
	text := update.Message.Text
	category := session.CurrentKey
	session.UserData[category] = strings.ToLower(text)
	session.CurrentKey = "" // Clear temporary choice

	msgText := fmt.Sprintf("Neat! Just so you know, this is what you already told me:\n%s\nYou can tell me more, or change your opinion on something.", factsToString(session.UserData))
	msg := tgbotapi.NewMessage(update.Message.Chat.ID, msgText)
	msg.ReplyMarkup = mainKeyboard
	bot.Send(msg)
	session.State = StateChoosing
}

// handleDone finishes the interaction.
func handleDone(update *tgbotapi.Update, session *UserSession, bot *tgbotapi.BotAPI) {
	session.CurrentKey = ""
	msgText := fmt.Sprintf("I learned these facts about you:\n%s\nUntil next time!", factsToString(session.UserData))
	msg := tgbotapi.NewMessage(update.Message.Chat.ID, msgText)
	msg.ReplyMarkup = tgbotapi.NewRemoveKeyboard(true)
	bot.Send(msg)

	// In the Python example, ConversationHandler.END is returned.
	// Here we just reset state to Choosing (waiting for start) or keep it in Choosing but without a keyboard.
	// To match persistence behavior strictly, we might leave the session active but waiting for /start.
	// For this implementation, we reset to 'Choosing' logically for the next interaction,
	// effectively waiting for a command or new text that matches filters.
	session.State = StateChoosing
}

// handleShowData displays gathered info (command handler).
func handleShowData(update *tgbotapi.Update, session *UserSession, bot *tgbotapi.BotAPI) {
	msgText := fmt.Sprintf("This is what you already told me:\n%s", factsToString(session.UserData))
	msg := tgbotapi.NewMessage(update.Message.Chat.ID, msgText)
	bot.Send(msg)
}

// ProcessUpdate routes the update based on state and content.
// This function is separated for testability.
func ProcessUpdate(update tgbotapi.Update, session *UserSession, bot *tgbotapi.BotAPI) {
	if update.Message == nil {
		return
	}

	text := update.Message.Text

	// Global Commands
	if update.Message.IsCommand() {
		switch update.Message.Command() {
		case "start":
			handleStart(&update, session, bot)
			return
		case "show_data":
			handleShowData(&update, session, bot)
			return
		}
	}

	// Regex Filters
	isDone := regexp.MustCompile("(?i)^Done$").MatchString(text)
	isRegular := regexp.MustCompile("^(Age|Favourite colour|Number of siblings)$").MatchString(text)
	isCustom := regexp.MustCompile("^Something else...$").MatchString(text)

	// State Machine
	switch session.State {
	case StateChoosing:
		if isRegular {
			handleRegularChoice(&update, session, bot)
		} else if isCustom {
			handleCustomChoice(&update, session, bot)
		} else if isDone {
			handleDone(&update, session, bot)
		} else {
			// Unknown input in Choosing state, re-show start or ignore
			// Python bot ignores unknown text in CHOOSING usually unless it matches regex
			log.Printf("[DEBUG] Ignored text in CHOOSING state: %s", text)
		}

	case StateTypingChoice:
		// Python logic: The text entering here becomes the 'choice' (category)
		// And we reuse 'regular_choice' logic which sets context.user_data["choice"]
		// and moves to TYPING_REPLY
		if !isDone { // Filter out "Done" if user changes mind? Python filters.TEXT & ~(COMMAND | Done)
			// Treat this text as the category name
			// Reuse regular_choice logic but purely for setting the key
			session.CurrentKey = strings.ToLower(text)
			replyText := fmt.Sprintf("Your %s? Yes, I would love to hear about that!", session.CurrentKey)
			msg := tgbotapi.NewMessage(update.Message.Chat.ID, replyText)
			bot.Send(msg)
			session.State = StateTypingReply
		} else {
			handleRegularChoice(&update, session, bot) // Fallback if they clicked a button instead of typing?
		}

	case StateTypingReply:
		if !isDone {
			handleReceivedInformation(&update, session, bot)
		} else {
			handleDone(&update, session, bot)
		}
	}
}

// --- Main ---

func main() {
	token := os.Getenv("TELEGRAM_TOKEN")
	if token == "" {
		log.Fatal("TELEGRAM_TOKEN environment variable is required")
	}

	// Initialize Storage
	// Ensure directory exists
	if err := os.MkdirAll("/data", 0755); err != nil {
		// Fallback for local run without docker volume mapping
		log.Println("[WARN] Could not create /data, using current directory for storage")
	}

	storagePath := StorageFile
	if _, err := os.Stat("/data"); os.IsNotExist(err) {
		storagePath = "conversationbot.json"
	}

	storage := NewStorage(storagePath)

	// Initialize Bot
	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		log.Panic(err)
	}

	bot.Debug = true
	log.Printf("Authorized on account %s", bot.Self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := bot.GetUpdatesChan(u)

	// Graceful Shutdown Channel
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-c
		log.Println("[INFO] Interrupt received, saving storage...")
		storage.Save()
		os.Exit(0)
	}()

	// Main Loop
	for update := range updates {
		if update.Message == nil {
			continue
		}

		userID := update.Message.From.ID
		session := storage.GetOrCreateSession(userID)

		log.Printf("[UPDATE] User: %s (%d) | Text: %s | Current State: %d", update.Message.From.UserName, userID, update.Message.Text, session.State)

		ProcessUpdate(update, session, bot)

		// Save on every update to ensure persistence (or use a ticker for performance)
		storage.Save()
	}
}

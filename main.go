package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/joho/godotenv"

	"github.com/craftlion/communautofinder"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
)

// Possible states in conversation with the bot
const (
	NotSearching = iota
	AskingType
	AskingMargin
	AskingPosition
	AskingDateStart
	AskingDateEnd
	AskingVehiculeType
	Searching
	EndSearch
)

type UserContext struct {
	chatId       int64
	state        int
	searchType   communautofinder.SearchType
	kmMargin     float64
	latitude     float64
	longitude    float64
	dateStart    time.Time
	dateEnd      time.Time
	vehiculeType communautofinder.VehiculeType
}

var userContexts = make(map[int64]UserContext)
var resultChannel = make(map[int64]chan int)
var cancelSearchingMethod = make(map[int64]context.CancelFunc)
var inputVehiculeTypes = map[rune]communautofinder.VehiculeType{'0': communautofinder.AllTypes, '1': communautofinder.FamilyCar, '2': communautofinder.UtilityVehicle, '3': communautofinder.MidSize, '4': communautofinder.Minivan}

const layoutDate = "2006-01-02 15:04"

const dateExample = "2023-11-21 20:12"

var bot *tgbotapi.BotAPI

var mutex = sync.Mutex{}

func main() {

	// Find TOKEN in .env file if exist
	godotenv.Load(".env")

	var err error

	bot, err = tgbotapi.NewBotAPI(os.Getenv("TOKEN_COMMUNAUTOSEARCH_BOT"))
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("Authorized on account %s", bot.Self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates, err := bot.GetUpdatesChan(u)

	if err != nil {
		log.Fatal(err)
	}

	for update := range updates {
		if update.Message == nil {
			continue
		}

		userID := update.Message.From.ID
		message := update.Message

		mutex.Lock()

		userCtx, found := userContexts[int64(userID)]
		userCtx.chatId = update.Message.Chat.ID

		if !found {
			resultChannel[userCtx.chatId] = make(chan int, 1)
		}

		response := generateResponse(&userCtx, message)

		userContexts[int64(userID)] = userCtx

		mutex.Unlock()

		msg := tgbotapi.NewMessage(userCtx.chatId, response)
		bot.Send(msg)
	}
}

func generateResponse(userCtx *UserContext, message *tgbotapi.Message) string {

	messageText := message.Text

	if strings.ToLower(messageText) == "/help" {
		return "Type:\n/start to initiate a new search\n/restart to restart a search with the same parameters as the previous search"
	} else if strings.ToLower(messageText) == "/start" {

		if userCtx.state == Searching {
			cancelSearchingMethod[userCtx.chatId]()
		}

		userCtx.state = AskingType
		return "Hello! Type:\n- station to search for a communauto station\n- flex to search for a communauto flex vehicle ?"
	} else if userCtx.state == AskingType {
		if strings.ToLower(messageText) == "station" {
			userCtx.searchType = communautofinder.SearchingStation
			userCtx.state = AskingMargin
			return "What is your search radius in Km ?"

		} else if strings.ToLower(messageText) == "flex" {
			userCtx.searchType = communautofinder.SearchingFlex
			userCtx.state = AskingMargin
			return "What is your search radius in Km ?"
		}

	} else if userCtx.state == AskingMargin {
		margin, err := strconv.ParseFloat(messageText, 64)

		if err == nil {

			if margin > 0 {
				userCtx.kmMargin = margin
				userCtx.state = AskingPosition
				return "Please share the GPS location for your search"
			}
		}

		return "Please enter a correct search radius"

	} else if userCtx.state == AskingPosition {
		if message.Location != nil {
			userCtx.latitude = message.Location.Latitude
			userCtx.longitude = message.Location.Longitude

			if userCtx.searchType == communautofinder.SearchingFlex {
				userCtx.state = Searching
				go launchSearch(*userCtx)
				return generateMessageResearch(*userCtx)

			} else if userCtx.searchType == communautofinder.SearchingStation {
				userCtx.state = AskingDateStart
				return fmt.Sprintf("What is the start date and time for the rental in the format %s ?", dateExample)
			}
		}
	} else if userCtx.state == AskingDateStart {

		t, err := time.Parse(layoutDate, messageText)

		if err == nil {
			userCtx.dateStart = t
			userCtx.state = AskingDateEnd
			return fmt.Sprintf("What is the end date and time for the rental in the format %s ?", dateExample)
		}

	} else if userCtx.state == AskingDateEnd {

		t, err := time.Parse(layoutDate, messageText)

		if err == nil {
			userCtx.dateEnd = t
			userCtx.state = AskingVehiculeType
			return "Select vehicule types :\n0 All types\n1 Family car\n2 Utility Vehicle\n3 MidSize\n4 Minivan"
		}

	} else if userCtx.state == AskingVehiculeType {
		inputValid := false
		userCtx.vehiculeType = communautofinder.AllTypes

		if len(messageText) == 1 {
			vehiculeType, found := inputVehiculeTypes[([]rune(messageText))[0]]

			if found {
				userCtx.vehiculeType = vehiculeType
				inputValid = true
			}
		}

		if inputValid {
			userCtx.state = Searching
			go launchSearch(*userCtx)
			return generateMessageResearch(*userCtx)
		}
		return "Please select one valid vehicule type"

	} else if strings.ToLower(messageText) == "/restart" {

		if userCtx.state == EndSearch {
			userCtx.state = Searching

			go launchSearch(*userCtx)
			return generateMessageResearch(*userCtx)
		} else {
			return "Please initiate a new search before restarting it."
		}

	}

	return "I didn't quite understand 😕"
}

func generateMessageResearch(userCtx UserContext) string {

	var typeSearch string

	if userCtx.searchType == communautofinder.SearchingFlex {
		typeSearch = "flex"
	} else if userCtx.searchType == communautofinder.SearchingStation {
		typeSearch = "station"
	}

	roundedKmMargin := int(userCtx.kmMargin)

	message := fmt.Sprintf("🔍 Searching for a %s vehicle within %dkm of the position you entered... you will receive a message when one is found", typeSearch, roundedKmMargin)

	if userCtx.searchType == communautofinder.SearchingStation {
		message += fmt.Sprintf(" from %s to %s", userCtx.dateStart.Format(layoutDate), userCtx.dateEnd.Format(layoutDate))
	}

	return message
}

func launchSearch(userCtx UserContext) {

	var currentCoordinate communautofinder.Coordinate = communautofinder.New(userCtx.latitude, userCtx.longitude)

	ctx, cancel := context.WithCancel(context.Background())

	cancelSearchingMethod[userCtx.chatId] = cancel

	if userCtx.searchType == communautofinder.SearchingFlex {
		go communautofinder.SearchFlexCarForGoRoutine(communautofinder.Montreal, currentCoordinate, userCtx.kmMargin, resultChannel[userCtx.chatId], ctx, cancel)
	} else if userCtx.searchType == communautofinder.SearchingStation {
		go communautofinder.SearchStationCarForGoRoutine(communautofinder.Montreal, currentCoordinate, userCtx.kmMargin, userCtx.dateStart, userCtx.dateEnd, userCtx.vehiculeType, resultChannel[userCtx.chatId], ctx, cancel)
	}

	nbCarFound := <-resultChannel[userCtx.chatId]

	var msg tgbotapi.MessageConfig

	if nbCarFound != -1 {
		msg = tgbotapi.NewMessage(userCtx.chatId, fmt.Sprintf("💡 Found ! %d vehicle(s) available according to your search criteria", nbCarFound))
	} else {
		msg = tgbotapi.NewMessage(userCtx.chatId, "😞 An error occurred in your search criteria. Please launch a new search")
	}

	bot.Send(msg)

	mutex.Lock()

	newUserCtx := userContexts[userCtx.chatId]
	newUserCtx.state = EndSearch
	userContexts[newUserCtx.chatId] = newUserCtx

	mutex.Unlock()

	delete(cancelSearchingMethod, userCtx.chatId)
}

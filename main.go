package communautofinder_telegrambot

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

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
	Searching
	EndSearch
)

const (
	Flex = iota
	Station
)

type UserContext struct {
	chatId     int64
	state      int
	searchType int
	kmMargin   float64
	latitude   float64
	longitude  float64
	dateStart  time.Time
	dateEnd    time.Time
}

const cityId = 59 // see available cities -> https://restapifrontoffice.reservauto.net/ReservautoFrontOffice/index.html?urls.primaryName=Branch%20version%202%20(6.93.1)#/

var userContexts = make(map[int64]UserContext)
var resultChannel = make(map[int64]chan int)
var cancelSearchingMethod = make(map[int64]context.CancelFunc)

const layoutDate = "2006-01-02 15:04"

var bot *tgbotapi.BotAPI

var mutex = sync.Mutex{}

func main() {

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
			resultChannel[userCtx.chatId] = make(chan int)
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
		return "Hello! Type:\n- station to search for a communauto station\n- flex to search for a communauto flex vehicle?"
	} else if userCtx.state == AskingType {
		if strings.ToLower(messageText) == "station" {
			userCtx.searchType = Station
			userCtx.state = AskingMargin
			return "What is your search radius in Km?"

		} else if strings.ToLower(messageText) == "flex" {
			userCtx.searchType = Flex
			userCtx.state = AskingMargin
			return "What is your search radius in Km?"
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

			if userCtx.searchType == Flex {
				userCtx.state = Searching
				go launchSearch(*userCtx)
				return generateMessageResearch(*userCtx)

			} else if userCtx.searchType == Station {
				userCtx.state = AskingDateStart

				return fmt.Sprintf("What is the start date and time for the rental in the format %s?", layoutDate)
			}
		}
	} else if userCtx.state == AskingDateStart {

		t, err := time.Parse(layoutDate, messageText)

		if err == nil {
			userCtx.dateStart = t
			userCtx.state = AskingDateEnd
			return fmt.Sprintf("What is the end date and time for the rental in the format %s?", layoutDate)
		}

	} else if userCtx.state == AskingDateEnd {

		t, err := time.Parse(layoutDate, messageText)

		if err == nil {
			userCtx.dateEnd = t
			userCtx.state = Searching
			go launchSearch(*userCtx)
			return generateMessageResearch(*userCtx)
		}

	} else if strings.ToLower(messageText) == "/restart" {

		if userCtx.state == EndSearch {
			userCtx.state = Searching

			go launchSearch(*userCtx)
			return generateMessageResearch(*userCtx)
		} else {
			return "Please initiate a new search before restarting it."
		}

	}

	return "I didn't quite understand."
}

func generateMessageResearch(userCtx UserContext) string {

	var typeSearch string

	if userCtx.searchType == Flex {
		typeSearch = "flex"
	} else if userCtx.searchType == Station {
		typeSearch = "station"
	}

	message := fmt.Sprintf("Searching for a %s vehicle within %fkm of GPS(%f,%f)", typeSearch, userCtx.kmMargin, userCtx.latitude, userCtx.longitude)

	if userCtx.searchType == Station {
		message += fmt.Sprintf(" from %s to %s", userCtx.dateStart, userCtx.dateEnd)
	}

	return message
}

func launchSearch(userCtx UserContext) {

	var currentCoordinate communautofinder.Coordinate = communautofinder.New(userCtx.latitude, userCtx.longitude)

	ctx, cancel := context.WithCancel(context.Background())

	cancelSearchingMethod[userCtx.chatId] = cancel

	if userCtx.searchType == Flex {
		go communautofinder.SearchFlexCarForGoRoutine(cityId, currentCoordinate, userCtx.kmMargin, resultChannel[userCtx.chatId], ctx)
	} else if userCtx.searchType == Station {
		go communautofinder.SearchStationCarForGoRoutine(cityId, currentCoordinate, userCtx.kmMargin, userCtx.dateStart, userCtx.dateEnd, resultChannel[userCtx.chatId], ctx)
	}

	nbCarFound := <-resultChannel[userCtx.chatId]

	if nbCarFound != -1 {
		msg := tgbotapi.NewMessage(userCtx.chatId, fmt.Sprintf("%d vehicle(s) available according to your search criteria", nbCarFound))
		bot.Send(msg)

		mutex.Lock()

		newUserCtx := userContexts[userCtx.chatId]
		newUserCtx.state = EndSearch
		userContexts[newUserCtx.chatId] = newUserCtx

		mutex.Unlock()
	}

	delete(cancelSearchingMethod, userCtx.chatId)
}

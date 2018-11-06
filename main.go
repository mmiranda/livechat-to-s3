package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/tidwall/gjson"
)

// LiveChatLogin stores your login on LiveChat dashboard
var LiveChatLogin = os.Getenv("LIVECHAT_LOGIN")

// LiveChatAPIKey stores your API KEY on LiveChat dashboard
var LiveChatAPIKey = os.Getenv("LIVECHAT_API_KEY")

var wg sync.WaitGroup

func main() {
	checkCredentials()
	totalPages := int(GetTotalPages())

	for i := 1; i <= totalPages; i++ {
		wg.Add(1)
		go getChatsByPage(i)
	}
	wg.Wait()

}

func getChatsByPage(page int) {

	// Iterates through all pages
	fmt.Printf("Getting chats from page %d \n", page)

	// Iterates through all chats in that page
	for _, chatID := range GetAllChats(page).Array() {
		wg.Add(1)
		go extractChatByID(chatID.String())
	}
	wg.Done()
}

func extractChatByID(chatID string) {
	// Gets info about specific chat
	originalChat := GetInfoAboutChat(chatID)
	createPath("./files/originals/")
	saveToFile("originals/"+chatID+".json", originalChat)
	transcriptChat(originalChat)
	wg.Done()
}

// RequestLiveChatAPI connects to LiveChatAPI and returns the specific result
func RequestLiveChatAPI(path string) string {
	client := &http.Client{}
	req, err := http.NewRequest("GET", "https://api.livechatinc.com/"+path, nil)
	if err != nil {
		log.Fatal(err)
	}
	req.Header.Set("X-API-Version", "2")
	req.SetBasicAuth(LiveChatLogin, LiveChatAPIKey)
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != 200 {
		log.Fatal(err)
	}
	bodyText, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Fatal(err)
	}

	return string(bodyText)
}

// GetTotalPages Gets total number of pages
func GetTotalPages() int64 {
	return gjson.Get(RequestLiveChatAPI("chats"), "pages").Int()
}

// GetAllChats reads all chat_id in every page
func GetAllChats(page int) gjson.Result {
	return gjson.Get(RequestLiveChatAPI("chats?page="+strconv.Itoa(page)), "chats.#.id")
}

// GetInfoAboutChat gets the raw text from an specific chat
func GetInfoAboutChat(chatID string) string {
	return RequestLiveChatAPI("chats/" + chatID)
}

// TranscriptChat extract the whole chat and write a transcription in a separate file
func transcriptChat(originalChat string) {
	visitorEmail := GetVisitorEmail(originalChat)

	createPath("./files/transcript/")
	createPath("./files/transcript/" + visitorEmail)

	fileName := time.Unix(gjson.Get(originalChat, "started_timestamp").Int(), 0).Format("2006-01-02 1504")
	header := "Original File is: ./originals/" + gjson.Get(originalChat, "id").String() + ".json\n"
	saveToFile("transcript/"+visitorEmail+"/"+fileName+".txt", header)

	messages := gjson.Get(originalChat, "events")
	messages.ForEach(func(key, value gjson.Result) bool {
		messageDetailed := gjson.GetMany(value.String(), "date", "author_name", "agent_id", "text")

		bufferMessage := messageDetailed[0].String() + " - [" +
			messageDetailed[1].String() + "|" +
			messageDetailed[2].String() + "]  " +
			messageDetailed[3].String()
		saveToFile("transcript/"+visitorEmail+"/"+fileName+".txt", bufferMessage)

		return true // keep iterating
	})

}

// GetVisitorEmail Gets the email of the visitor or a fallback if visitor does not have email key
func GetVisitorEmail(originalChat string) string {
	result := gjson.Get(originalChat, "visitor.email")

	var visitorEmail string
	if result.Exists() {
		visitorEmail = result.String()
	} else {
		// SET A FALLBACK IN CASE EMAIL KEY DOES NOT EXISTS
		result = gjson.Get(originalChat, "prechat_survey.#[key==\"E-mail:\"].value")
		if result.Exists() {
			visitorEmail = result.String()
		} else {
			visitorEmail = "unknown"
		}
	}

	return visitorEmail
}

func createPath(path string) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		os.Mkdir(path, 0777)
	}
}

func saveToFile(fileName string, content string) {
	f, err := os.OpenFile("./files/"+fileName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(content + "\n"); err != nil {
		log.Fatal(err)
	}

}

func checkCredentials() {
	if len(LiveChatLogin) == 0 || len(LiveChatAPIKey) == 0 {
		log.Fatal("Missing LiveChat credentials")
	}
}

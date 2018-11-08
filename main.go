package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/tidwall/gjson"
	"gopkg.in/cheggaaa/pb.v1"
)

// LiveChatLogin stores your login on LiveChat dashboard
var LiveChatLogin = os.Getenv("LIVECHAT_LOGIN")

// LiveChatAPIKey stores your API KEY on LiveChat dashboard
var LiveChatAPIKey = os.Getenv("LIVECHAT_API_KEY")

const (
	s3BucketName   = "your-bucket-name"
	s3BucketRegion = "eu-west-1"
	// Leave this empty if you want to save on root
	s3BucketPath = ""
)

var wg sync.WaitGroup
var (
	concurrency         = 3
	concurrencyDetailed = 3
	concurrencyS3       = 50
	semaChan            = make(chan bool, concurrency)
	semaChanDetailed    = make(chan bool, concurrencyDetailed)
	semaChanS3          = make(chan bool, concurrencyS3)
)

func main() {
	checkCredentials()
	totalPages := int(GetTotalPages())
	bar := pb.StartNew(totalPages).Prefix("Extracting pages")
	for i := 1; i <= totalPages; i++ {
		bar.Increment()
		semaChan <- true // block while full
		wg.Add(1)
		go getChatsByPage(i)
	}
	wg.Wait()
	bar.FinishPrint("All files exported!")

}

func getChatsByPage(page int) {
	defer func() {
		<-semaChan // read releases a slot
	}()
	// fmt.Printf("Getting chats from page %d \n", page)

	// Iterates through all chats in that page
	for _, chatID := range GetAllChats(page).Array() {
		semaChanDetailed <- true // block while full
		wg.Add(1)
		go extractChatByID(chatID.String())
	}
	wg.Done()
}

func extractChatByID(chatID string) {
	defer func() {
		<-semaChanDetailed // read releases a slot
	}()
	// Gets info about specific chat
	originalChat := GetInfoAboutChat(chatID)
	createPath("./files/originals/")
	saveToFile("originals/"+chatID+".json", originalChat)

	semaChanS3 <- true // block while full
	wg.Add(1)
	go uploadToS3("./files/originals/"+chatID+".json", s3BucketPath+"/originals/")

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
		fmt.Printf("request path: %s\n", path)
		fmt.Println(resp)
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

		// semaChanS3 <- true // block while full
		wg.Add(1)
		go uploadToS3("./files/transcript/"+visitorEmail+"/"+fileName+".txt", s3BucketPath+"/transcript/"+visitorEmail+"/")

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
		if result.Exists() && len(result.String()) > 0 {
			visitorEmail = result.String()
		} else {
			visitorEmail = "unknown"
		}
	}

	return cleanCharacters(visitorEmail)
}

// Necessary because people often type their own email wrong
func cleanCharacters(str string) string {
	reg, err := regexp.Compile("[^a-zA-Z0-9\\-@+._]+")
	if err != nil {
		log.Fatal(err)
	}
	return reg.ReplaceAllString(str, "")
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

func uploadToS3(localFile string, s3Path string) {
	defer func() {
		<-semaChanS3 // read releases a slot
	}()

	creds := credentials.NewSharedCredentials("", "")
	_, err := creds.Get()
	if err != nil {
		log.Fatal(err)
	}

	config := aws.NewConfig().WithRegion(s3BucketRegion).WithCredentials(creds)
	s3Session := s3.New(session.New(), config)

	file, err := os.Open(localFile)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()
	fileInfo, _ := file.Stat()
	size := fileInfo.Size()
	buffer := make([]byte, size)

	file.Read(buffer)
	fileBytes := bytes.NewReader(buffer)
	fileType := http.DetectContentType(buffer)

	_, fileName := path.Split(file.Name())
	path := s3Path + fileName

	params := &s3.PutObjectInput{
		Bucket:               aws.String(s3BucketName),
		Key:                  aws.String(path),
		Body:                 fileBytes,
		ContentLength:        aws.Int64(size),
		ContentType:          aws.String(fileType),
		ServerSideEncryption: aws.String("AES256"),
	}
	_, err = s3Session.PutObject(params)
	if err != nil {
		log.Fatal(err)
	}
	wg.Done()
}

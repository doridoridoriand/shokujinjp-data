package main

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/ChimeraCoder/anaconda"
	"github.com/shokujinjp/shokujinjp-sdk-go/shokujinjp"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/vision/v1"
)

const (
	createAtFormat = "Mon Jan 02 15:04:05 -0700 2006"
	dayFormat      = "2006-01-02"
	idFormat       = "20060102"
	weeklyFileName = "../weekly.csv"
	twitterId      = "shokujinjp"
)

var (
	re           = regexp.MustCompile(`(9|15)\.(.*?)(\d+)円`)
	twitterQuery = []string{"今週の週替わり定食", "今週の週変わり定食", "#食神週替わり定食"}
)

func initialize() (*vision.Service, *anaconda.TwitterApi, error) {
	// create vision service
	saJson := os.Getenv("SA_JSON")

	vcfg, err := google.JWTConfigFromJSON(
		[]byte(saJson), vision.CloudPlatformScope)
	if err != nil {
		return nil, nil, err
	}

	vclient := vcfg.Client(context.Background())

	svc, err := vision.New(vclient)
	if err != nil {
		return nil, nil, err
	}

	// create twitter client
	api := anaconda.NewTwitterApiWithCredentials(os.Getenv("TW_AT"), os.Getenv("TW_ATS"), os.Getenv("TW_CK"), os.Getenv("TW_CS"))

	return svc, api, nil
}

func getNewestTweet(api *anaconda.TwitterApi) (anaconda.Tweet, error) {
	fromQuery := " from:" + twitterId
	result := anaconda.Tweet{
		Text: "null",
	}
	var resultTime time.Time

	for _, query := range twitterQuery {
		q := query + fromQuery
		searchResult, err := api.GetSearch(q, nil)
		if err != nil {
			return anaconda.Tweet{}, err
		}

		// get newest tweet
		if len(searchResult.Statuses) < 1 {
			log.Println("missing tweet")
			continue
		}

		// now Official Twitter API return only 7 days ago.
		// maybe, searchResult.Statues is only 1
		tweet := searchResult.Statuses[0]
		tweetTime, err := tweet.CreatedAtTime()
		if err != nil {
			log.Printf("failed to parse time : %s￿￿￿￿￿￿￿", err)
			continue
		}
		if result.Text == "null" {
			// not set result
			result = tweet
		} else if tweetTime.After(resultTime) {
			// tweetTime is new then resultTime
			result = tweet
			resultTime = tweetTime
		}

	}

	if result.Text == "null" {
		// not found!
		return anaconda.Tweet{}, errors.New("missing tweet")
	}

	return result, nil
}

func doVisionRequest(svc *vision.Service, imageURL string) (*vision.BatchAnnotateImagesResponse, error) {
	imgSource := &vision.ImageSource{
		ImageUri: imageURL,
	}
	img := &vision.Image{Source: imgSource}
	feature := &vision.Feature{
		Type:       "DOCUMENT_TEXT_DETECTION",
		MaxResults: 10,
	}
	req := &vision.AnnotateImageRequest{
		Image:    img,
		Features: []*vision.Feature{feature},
	}
	batch := &vision.BatchAnnotateImagesRequest{
		Requests: []*vision.AnnotateImageRequest{req},
	}
	res, err := svc.Images.Annotate(batch).Do()
	if err != nil {
		return nil, err
	}

	return res, nil
}

func alreadyDone(day string) (bool, error) {
	fp, err := os.Open(weeklyFileName)
	if err != nil {
		return false, err
	}
	defer fp.Close()

	reader := csv.NewReader(fp)
	reader.Comma = ','

	records, err := reader.ReadAll()
	if err != nil {
		return false, err
	}

	for _, v := range records[1:] {
		if day == v[0][:8] {
			return true, nil
		}
	}

	return false, nil

}

func parseOneLine(oneline string, t time.Time) (menu9 shokujinjp.Menu, menu15 shokujinjp.Menu, err error) {
	slice915 := re.FindAllStringSubmatch(oneline, -1)

	if len(slice915) < 2 {
		errStr := fmt.Sprintf("failed to parse string: %v", slice915)

		return shokujinjp.Menu{}, shokujinjp.Menu{}, errors.New(errStr)
	}

	menu9 = shokujinjp.Menu{
		Id:          t.Format(idFormat) + "09",
		Name:        slice915[0][2],
		Price:       slice915[0][3],
		Category:    "定食",
		Description: "週代わり定食9番",
		DayStart:    t.Format(dayFormat),
		DayEnd:      t.AddDate(0, 0, 6).Format(dayFormat),
	}
	menu15 = shokujinjp.Menu{
		Id:          t.Format(idFormat) + "15",
		Name:        slice915[1][2],
		Price:       slice915[1][3],
		Category:    "定食",
		Description: "週代わり定食15番",
		DayStart:    t.Format(dayFormat),
		DayEnd:      t.AddDate(0, 0, 6).Format(dayFormat),
	}

	return menu9, menu15, nil
}

func writeNewMenu(menu9 shokujinjp.Menu, menu15 shokujinjp.Menu) error {
	fp, err := os.OpenFile(weeklyFileName, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer fp.Close()

	writer := csv.NewWriter(fp)
	writer.Comma = ','

	err = writer.Write(menu9.MarshalStringSlice())
	if err != nil {
		return err
	}

	err = writer.Write(menu15.MarshalStringSlice())
	if err != nil {
		return err
	}

	writer.Flush()
	return nil

}

func main() {
	visionSvc, api, err := initialize()
	if err != nil {
		log.Fatal(err)
	}

	tweet, err := getNewestTweet(api)
	if err != nil {
		log.Fatal(err)
	}

	t, err := time.Parse(createAtFormat, tweet.CreatedAt)
	if err != nil {
		log.Fatal(err)
	}

	done, err := alreadyDone(t.Format(idFormat))
	if err != nil {
		log.Fatal(err)
	}
	if done == true {
		log.Println("already done")
		os.Exit(0)
	}

	res, err := doVisionRequest(visionSvc, tweet.Entities.Media[0].Media_url_https)
	if err != nil {
		log.Fatal(err)
	}
	rawText := res.Responses[0].FullTextAnnotation.Text

	var oneline string
	for _, s := range rawText {
		o := string([]rune{s})
		oneline += strings.TrimSpace(o)
	}

	menu9, menu15, err := parseOneLine(oneline, t)
	if err != nil {
		log.Fatal(err)
	}

	err = writeNewMenu(menu9, menu15)
	if err != nil {
		log.Fatal(err)
	}
}

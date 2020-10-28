package p

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"time"

	"mime/multipart"

	"cloud.google.com/go/storage"
)

// GCSEvent is the payload of a GCS event. Please refer to the docs for
// additional information regarding GCS events.
type GCSEvent struct {
	Bucket string `json:"bucket"`
	Name   string `json:"name"`
}

type Log struct {
	InsertId         string                 `json:"insertId"`
	LogName          string                 `json:"logName"`
	ReceiveTimestamp string                 `json:"receiveTimestamp"`
	Resource         Resource               `json:"resource"`
	TextPayload      string                 `json:"textPayload"`
	JSONPayload      map[string]interface{} `json:"jsonPayload"`
}

type Resource struct {
	Labels map[string]interface{} `json:"labels"`
	Type   string                 `json:"type"`
}

var webhookURL = os.Getenv("WEBHOOK_URL")

func NotifyToDiscord(ctx context.Context, e GCSEvent) error {
	log.Printf("Processing file: %s", e.Name)
	c, err := storage.NewClient(ctx)
	if err != nil {
		log.Printf("[error] new client failed, %s", err)
		return nil
	}

	bkt := c.Bucket(e.Bucket)
	obj := bkt.Object(e.Name)

	r, err := obj.NewReader(ctx)
	if err != nil {
		log.Printf("[error] NewReader failed, %s", err)
		return nil
	}
	defer r.Close()

	text := &bytes.Buffer{}
	d := json.NewDecoder(r)
	var logLines int
	for d.More() {
		logLines++
		var logData Log
		if err := d.Decode(&logData); err != nil {
			log.Printf("[error] Decode failed, %s", err)
			continue
		}
		if logData.TextPayload != "" {
			text.WriteString(logData.TextPayload + "\n")
		}
		if logData.JSONPayload != nil {
			jtxt, err := json.Marshal(logData.JSONPayload)
			if err != nil {
				log.Printf("[error] Marshal jsonPayload failed, %s", err)
				continue
			}
			text.Write(jtxt)
			text.WriteString("\n")
		}
	}

	comment := fmt.Sprintf("log length: %d", logLines)
	if logLines >= 50 {
		comment = fmt.Sprintf("@everyone %s", comment)
	}

	if err := postToDiscord(comment, text); err != nil {
		log.Printf("[error] postToDiscord failed, %s", err)
		return nil
	}

	return nil
}

func postToDiscord(message string, body io.Reader) error {
	b := &bytes.Buffer{}
	mw := multipart.NewWriter(b)
	fw, err := mw.CreateFormFile("file", genFilename())
	if err != nil {
		return fmt.Errorf("[error] failed to CreateFormFile: %s", err)
	}
	if _, err := io.Copy(fw, body); err != nil {
		return fmt.Errorf("[error] failed to io.Copy: %s", err)
	}

	ffw, err := mw.CreateFormField("content")
	if err != nil {
		return fmt.Errorf("[error] failed to CreateFormField: %s", err)
	}
	if _, err := ffw.Write([]byte(message)); err != nil {
		return fmt.Errorf("[error] failed to Write: %s", err)
	}

	contentType := mw.FormDataContentType()
	if err := mw.Close(); err != nil {
		return fmt.Errorf("[error] failed to Close: %s", err)
	}

	req, err := http.NewRequest(http.MethodPost, webhookURL, b)
	if err != nil {
		return fmt.Errorf("[error] failed to NewRequest: %s", err)
	}
	req.Header.Set("Content-Type", contentType)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("[error] failed to http.Do: %s", err)
	}
	defer resp.Body.Close()

	res, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("error: %s", string(res))
	}
	return nil
}

func genFilename() string {
	now := time.Now()
	return fmt.Sprintf("errlogs-%d-%d-%d-%d-%d-%d.txt", now.Year(), int(now.Month()), now.Day(), now.Hour(), now.Minute(), now.Second())
}

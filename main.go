package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/go-chi/render"
	"golang.org/x/net/context/ctxhttp"
)

var hostFlag = flag.String("host", ":7000", "host to bind to")

type AlertMessage struct {
	Version           string            `json:"version"`
	GroupKey          string            `json:"groupKey"`
	Status            string            `json:"status"`
	Receiver          string            `json:"receiver"`
	GroupLabels       map[string]string `json:"groupLabels"`
	CommonLabels      map[string]string `json:"commonLabels"`
	CommonAnnotations map[string]string `json:"commonAnnotations"`
	ExternalURL       string            `json:"externalURL"`
	Alerts            []Alert           `json:"alerts"`
}

type Alert struct {
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	StartsAt     string            `json:"startsAt"`
	EndsAt       string            `json:"endsAt"`
	GeneratorURL string            `json:"generatorURL"`
}

type DiscordWebhook struct {
	Content string      `json:"content"`
	Embeds  []RichEmbed `json:"embeds"`
}

type RichEmbed struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Color       int    `json:"color"`
}

func main() {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

	r.Post("/{id}/{token}", handleWebhook)

	http.ListenAndServe(*hostFlag, r)
}

func handleWebhook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	body := AlertMessage{}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.WriteHeader(400)
		return
	}

	alertname, ok := body.GroupLabels["alertname"]
	if !ok {
		w.WriteHeader(400)
		return
	}
	labels := []string{}
	for k := range body.GroupLabels {
		if k != "alertname" {
			labels = append(labels, k)
		}
	}
	sort.Strings(labels)
	for i, k := range labels {
		labels[i] = fmt.Sprintf("%s = %s", k, body.GroupLabels[k])
	}

	title := fmt.Sprintf("[%s:%d] %s (%s)", strings.ToUpper(body.Status), len(body.Alerts), alertname, strings.Join(labels, ", "))

	descs := []string{}
	for _, alert := range body.Alerts {
		summary, ok := alert.Annotations["summary"]
		if ok {
			descs = append(descs, fmt.Sprintf("- %s", summary))
		}
	}

	color := 0xFF0000
	if body.Status == "resolved" {
		color = 0x00FF00
	}

	embed := RichEmbed{
		Title:       title,
		Description: strings.Join(descs, "\n"),
		Color:       color,
	}

	payload := DiscordWebhook{
		Content: r.URL.Query().Get("pretext"),
		Embeds:  []RichEmbed{embed},
	}

	jsonString, err := json.Marshal(payload)
	if err != nil {
		w.WriteHeader(500)
		return
	}

	url := fmt.Sprintf("https://discordapp.com/api/webhooks/%s/%s", chi.URLParam(r, "id"), chi.URLParam(r, "token"))

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonString))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := ctxhttp.Do(ctx, client, req)

	if err != nil {
		w.WriteHeader(500)
		return
	}

	render.Status(r, resp.StatusCode)
}

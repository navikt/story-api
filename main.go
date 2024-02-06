package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"

	"github.com/navikt/story-api/pkg/api"
)

func main() {
	var bucketName string
	var nadaBackendURL string
	var nadaBackendToken string
	flag.StringVar(&bucketName, "bucket", os.Getenv("STORY_BUCKET"), "The storage bucket for the story content")
	flag.StringVar(&nadaBackendURL, "nada-backend-url", os.Getenv("NADA_BACKEND_URL"), "NADA backend URL")
	flag.StringVar(&nadaBackendToken, "nada-backend-token", os.Getenv("NADA_BACKEND_TOKEN"), "Token for fetching team/token mapping from nada-backend")
	flag.Parse()

	ctx := context.Background()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	tokenTeamMap, err := fetchNadaTokens(nadaBackendURL, nadaBackendToken)
	if err != nil {
		logger.Error("fetching token team mapping from nada-backend", "error", err)
		os.Exit(1)
	}

	router, err := api.New(ctx, bucketName, tokenTeamMap, logger.With("subsystem", "api"))
	if err != nil {
		logger.Error("creating api", "error", err)
		os.Exit(1)
	}

	server := http.Server{
		Addr:    ":8080",
		Handler: router,
	}

	if err := server.ListenAndServe(); err != nil {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func fetchNadaTokens(url, token string) (map[string]string, error) {
	r, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	r.Header.Add("Authorization", fmt.Sprintf("Bearer %v", token))

	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		return nil, err
	}

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	teamTokenMap := map[string]string{}
	if err := json.Unmarshal(respBytes, &teamTokenMap); err != nil {
		return nil, err
	}

	return teamTokenMap, nil
}

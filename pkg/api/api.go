package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"cloud.google.com/go/storage"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/navikt/story-api/pkg/gcs"
)

const (
	maxMemoryMultipartForm = 32 << 20
	metadataFile           = "nada_metadata.json"
)

type storyMetadata struct {
	Title     string   `json:"title"`
	Slug      string   `json:"slug"`
	ID        string   `json:"id"`
	Team      string   `json:"team"`
	Published string   `json:"published"`
	Tags      []string `json:"tags"`
}

func New(ctx context.Context, bucket string, tokenTeamMap map[string]string, logger *slog.Logger) (*echo.Echo, error) {
	gcs, err := gcs.New(ctx, bucket)
	if err != nil {
		return nil, err
	}

	server := echo.New()
	v1 := server.Group("/api/v1")
	setupRoutes(v1, gcs, tokenTeamMap, logger)

	return server, nil
}

func setupRoutes(server *echo.Group, gcs *gcs.Client, tokenTeamMap map[string]string, logger *slog.Logger) {
	server.POST("/story", func(c echo.Context) error {
		token, err := teamTokenFromHeader(c)
		if err != nil {
			return c.JSON(http.StatusUnauthorized, map[string]string{
				"status":  "error",
				"message": "no authorization token provided with request",
			})
		}

		team, ok := tokenTeamMap[token]
		if !ok {
			return c.JSON(http.StatusUnauthorized, map[string]string{
				"status":  "error",
				"message": "invalid token provided with request",
			})
		}

		bodyBytes, err := io.ReadAll(c.Request().Body)
		if err != nil {
			return err
		}

		storyMeta := storyMetadata{}
		if err := json.Unmarshal(bodyBytes, &storyMeta); err != nil {
			logger.Error("unmarshalling story metadata", "error", err)
			return c.JSON(http.StatusBadRequest, map[string]string{
				"status":  "error",
				"message": "unmarshalling story metadata",
			})
		}
		storyMeta.Team = team
		storyMeta.ID = uuid.New().String()
		storyMeta.Slug = createStorySlug(storyMeta)

		_, err = gcs.ReadFile(c.Request().Context(), fmt.Sprintf("fortelling/%v/%v", storyMeta.Slug, metadataFile))
		if err == nil {
			logger.Error(fmt.Sprintf("there already exists a story with slug %v", storyMeta.Slug))
			return c.JSON(http.StatusConflict, map[string]string{
				"status":  "error",
				"message": fmt.Sprintf("there already exists a story %v", storyMeta.Slug),
			})
		} else if !errors.Is(err, storage.ErrObjectNotExist) {
			logger.Error(fmt.Sprintf("checking existance of story with slug %v", storyMeta.Slug), "error", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{
				"status":  "error",
				"message": "internal server error",
			})
		}

		storyMetaBytes, err := json.Marshal(storyMeta)
		if err != nil {
			logger.Error(fmt.Sprintf("marshalling story metadata %v", storyMeta.Slug), "error", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{
				"status":  "error",
				"message": "internal server error",
			})
		}
		if err := gcs.UploadFile(c.Request().Context(), fmt.Sprintf("fortelling/%v/%v", storyMeta.Slug, metadataFile), storyMetaBytes); err != nil {
			logger.Error(fmt.Sprintf("unable to create story %v", storyMeta.Slug), "error", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{
				"status":  "error",
				"message": fmt.Sprintf("unable to create story %v", storyMeta.Slug),
			})
		}

		return c.JSON(http.StatusCreated, map[string]string{
			"status":  "created",
			"message": fmt.Sprintf("created story with id '%v'", storyMeta.ID),
		})
	})

	server.PUT("/story/:id", func(c echo.Context) error {
		storyID := c.Param("id")

		storyMeta, err := getStoryMetadataForID(c.Request().Context(), gcs, storyID)
		if err != nil {
			logger.Error(fmt.Sprintf("getting story metadata for story with ID %v", storyID), "error", err)
			return c.JSON(http.StatusNotFound, map[string]string{
				"status":  "error",
				"message": "internal server error",
			})
		}
		if storyMeta == nil {
			logger.Error(fmt.Sprintf("no story exists for id %v", storyID), "error", err)
			return c.JSON(http.StatusNotFound, map[string]string{
				"status":  "error",
				"message": fmt.Sprintf("no story exists for id %v", storyID),
			})
		}

		token, err := teamTokenFromHeader(c)
		if err != nil {
			return c.JSON(http.StatusUnauthorized, map[string]string{
				"status":  "error",
				"message": "no authorization token provided with request",
			})
		}

		team, ok := tokenTeamMap[token]
		if !ok {
			return c.JSON(http.StatusUnauthorized, map[string]string{
				"status":  "error",
				"message": "invalid token provided with request",
			})
		}

		if storyMeta.Team != team {
			return c.JSON(http.StatusUnauthorized, map[string]string{
				"status":  "error",
				"message": fmt.Sprintf("team %v is not authorized to update story with slug %v", team, storyMeta.Slug),
			})
		}

		if err := deleteStoryFiles(c.Request().Context(), gcs, fmt.Sprintf("fortelling/%v", storyMeta.Slug)); err != nil {
			logger.Error(fmt.Sprintf("error deleting story '%v' before updating", storyMeta.Slug), "error", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{
				"status":  "error",
				"message": fmt.Sprintf("error deleting story '%v' before updating", storyMeta.Slug),
			})
		}

		if err := uploadStoryFiles(c, gcs, *storyMeta); err != nil {
			logger.Error(fmt.Sprintf("error uploading story with slug '%v'", storyMeta.Slug), "error", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{
				"status":  "error",
				"message": fmt.Sprintf("error uploading story with slug '%v' to bucket", storyMeta.Slug),
			})
		}

		return c.JSON(http.StatusOK, map[string]string{
			"status":  "updated",
			"message": fmt.Sprintf("updated story %v", storyMeta.Slug),
		})
	})

	server.PATCH("/story/:id", func(c echo.Context) error {
		storyID := c.Param("id")

		storyMeta, err := getStoryMetadataForID(c.Request().Context(), gcs, storyID)
		if err != nil {
			logger.Error(fmt.Sprintf("getting story metadata for story with ID %v", storyID), "error", err)
			return c.JSON(http.StatusNotFound, map[string]string{
				"status":  "error",
				"message": "internal server error",
			})
		}
		if storyMeta == nil {
			logger.Error(fmt.Sprintf("no story exists for id %v", storyID), "error", err)
			return c.JSON(http.StatusNotFound, map[string]string{
				"status":  "error",
				"message": fmt.Sprintf("no story exists for id %v", storyID),
			})
		}

		token, err := teamTokenFromHeader(c)
		if err != nil {
			return c.JSON(http.StatusUnauthorized, map[string]string{
				"status":  "error",
				"message": "no authorization token provided with request",
			})
		}

		team, ok := tokenTeamMap[token]
		if !ok {
			return c.JSON(http.StatusUnauthorized, map[string]string{
				"status":  "error",
				"message": "invalid token provided with request",
			})
		}

		if storyMeta.Team != team {
			return c.JSON(http.StatusUnauthorized, map[string]string{
				"status":  "error",
				"message": fmt.Sprintf("team %v is not authorized to update story with slug %v", team, storyMeta.Slug),
			})
		}

		if err := uploadStoryFiles(c, gcs, *storyMeta); err != nil {
			logger.Error(fmt.Sprintf("error uploading story with slug '%v'", storyMeta.Slug), "error", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{
				"status":  "error",
				"message": fmt.Sprintf("error uploading story with slug '%v' to bucket", storyMeta.Slug),
			})
		}

		return c.JSON(http.StatusOK, map[string]string{
			"status":  "updated",
			"message": fmt.Sprintf("updated story %v", storyMeta.Slug),
		})
	})
}

func teamTokenFromHeader(c echo.Context) (string, error) {
	authHeader := c.Request().Header.Get("Authorization")
	authHeaderParts := strings.Split(authHeader, " ")
	if len(authHeaderParts) != 2 {
		return "", errors.New("invalid authorization header")
	}

	return authHeaderParts[1], nil
}

func createStorySlug(storyMeta storyMetadata) string {
	if storyMeta.Slug != "" {
		return url.QueryEscape(storyMeta.Slug)
	} else if storyMeta.Title != "" {
		return url.QueryEscape(storyMeta.Title)
	}

	return storyMeta.ID
}

func getStoryMetadataForID(ctx context.Context, gcs *gcs.Client, id string) (*storyMetadata, error) {
	files, err := gcs.ListFilesWithGlobalPattern(ctx, fmt.Sprintf("**%v**", metadataFile))
	if err != nil {
		return nil, err
	}

	for _, file := range files {
		metaBytes, err := gcs.ReadFile(ctx, file)
		if err != nil {
			return nil, err
		}

		storyMeta := storyMetadata{}
		if err := json.Unmarshal(metaBytes, &storyMeta); err != nil {
			return nil, err
		}

		if storyMeta.ID == id {
			return &storyMeta, nil
		}
	}

	return nil, nil
}

func deleteStoryFiles(ctx context.Context, gcs *gcs.Client, storySlug string) error {
	files := gcs.ListFilesWithPrefix(ctx, storySlug)

	for _, file := range files {
		if !strings.Contains(file, metadataFile) {
			if err := gcs.DeleteFile(ctx, file); err != nil {
				return err
			}
		}
	}

	return nil
}

func uploadStoryFiles(c echo.Context, gcs *gcs.Client, storyMeta storyMetadata) error {
	if err := c.Request().ParseMultipartForm(maxMemoryMultipartForm); err != nil {
		return err
	}

	for fileName, fileHeader := range c.Request().MultipartForm.File {
		file, err := fileHeader[0].Open()
		if err != nil {
			return err
		}
		fileBytes, err := io.ReadAll(file)
		if err != nil {
			return err
		}

		if err := gcs.UploadFile(c.Request().Context(), fmt.Sprintf("fortelling/%v/%v", storyMeta.Slug, fileName), fileBytes); err != nil {
			return err
		}
	}
	return nil
}

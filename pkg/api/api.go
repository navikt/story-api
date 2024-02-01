package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/navikt/story-api/pkg/gcs"
)

const (
	maxMemoryMultipartForm = 32 << 20
)

type story struct {
	ID   string `json:"name"`
	Team string `json:"team"`
}

func New(ctx context.Context, bucket string, logger *slog.Logger) (*echo.Echo, error) {
	gcs, err := gcs.New(ctx, bucket)
	if err != nil {
		return nil, err
	}

	server := echo.New()
	v1 := server.Group("/api/v1")
	setupRoutes(v1, gcs, logger)

	return server, nil
}

func setupRoutes(server *echo.Group, gcs *gcs.Client, logger *slog.Logger) {
	server.POST("/story", func(c echo.Context) error {
		// token, err := teamTokenFromHeader(c)
		// if err != nil {
		// 	return c.JSON(http.StatusUnauthorized, map[string]string{
		// 		"status":  "error",
		// 		"message": fmt.Sprintf("no authorization token provided with request"),
		// 	})
		// }

		// todo: get tokens from markedsplassen and derive team from token provided with request
		team := "nada"
		storyID := uuid.New()
		newStory := story{
			ID:   storyID.String(),
			Team: team,
		}

		if err := uploadStory(c, gcs, newStory); err != nil {
			logger.Error(fmt.Sprintf("unable to upload story %v to bucket", storyID), "error", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{
				"status":  "error",
				"message": fmt.Sprintf("unable to upload story %v to bucket", storyID),
			})
		}

		return c.JSON(http.StatusCreated, map[string]string{
			"status":  "created",
			"message": fmt.Sprintf("created story with id '%v'", storyID),
		})
	})

	server.PUT("/story/:id", func(c echo.Context) error {
		storyID := c.Param("id")

		// token, err := teamTokenFromHeader(c)
		// if err != nil {
		// 	return c.JSON(http.StatusUnauthorized, map[string]string{
		// 		"status":  "error",
		// 		"message": fmt.Sprintf("no authorization token provided with request"),
		// 	})
		// }

		// todo: get tokens from markedsplassen and derive team from token provided with request
		team := "nada"
		storyMetadata, err := gcs.GetObjectMetadata(c.Request().Context(), storyID)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{
				"status":  "error",
				"message": fmt.Sprintf("metadata for index page not found for story %v", storyID),
			})
		}

		storyOwner, ok := storyMetadata["team"]
		if !ok {
			logger.Error("no team is registered for story", "story_id", storyID)
			return c.JSON(http.StatusInternalServerError, map[string]string{
				"status":  "error",
				"message": fmt.Sprintf("no team is registered for story with id '%v'", storyID),
			})
		}
		if storyOwner != team {
			return c.JSON(http.StatusUnauthorized, map[string]string{
				"status":  "error",
				"message": fmt.Sprintf("team %v is not authorized to update story with id %v", team, storyID),
			})
		}

		if err := gcs.DeleteFilesWithPrefix(c.Request().Context(), fmt.Sprintf("datafortellinger/%v", storyID)); err != nil {
			logger.Error(fmt.Sprintf("error deleting story '%v' before updating", storyID), "error", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{
				"status":  "error",
				"message": fmt.Sprintf("error deleting story '%v' before updating", storyID),
			})
		}

		if err := uploadStory(c, gcs, story{ID: storyID, Team: team}); err != nil {
			logger.Error(fmt.Sprintf("error uploading story '%v'", storyID), "error", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{
				"status":  "error",
				"message": fmt.Sprintf("error uploading story with id '%v' to bucket", storyID),
			})
		}

		return c.JSON(http.StatusOK, map[string]string{
			"status":  "updated",
			"message": fmt.Sprintf("updated story %v", storyID),
		})
	})
}

func uploadStory(c echo.Context, gcs *gcs.Client, s story) error {
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

		if err := gcs.UploadFile(c.Request().Context(), fmt.Sprintf("datafortellinger/%v/%v", s.ID, fileName), fileBytes, s.Team); err != nil {
			return err
		}
	}
	return nil
}

func teamTokenFromHeader(c echo.Context) (string, error) {
	authHeader := c.Request().Header.Get("Authorization")
	authHeaderParts := strings.Split(authHeader, " ")
	if len(authHeaderParts) != 2 {
		return "", errors.New("invalid authorization header")
	}

	return authHeaderParts[1], nil
}

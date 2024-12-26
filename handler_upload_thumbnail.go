package main

import (
	"fmt"
	"net/http"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
        "io"
        "encoding/base64"
)

func (cfg *apiConfig) handlerUploadThumbnail(w http.ResponseWriter, r *http.Request) {
	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}


	fmt.Println("uploading thumbnail for video", videoID, "by user", userID)

	// TODO: implement the upload here
        const maxMemory = 10 << 20
        
        err = r.ParseMultipartForm(maxMemory)
        if err != nil {
            respondWithError(w, http.StatusInternalServerError, "Error while parsing form:", err)
            return
        }

        multipartFile, multipartFileHeader, err := r.FormFile("thumbnail")
        if err != nil {
            respondWithError(w, http.StatusInternalServerError, "Error while reading form:", err)
            return
        }

        mediaType := multipartFileHeader.Header["Content-Type"][0]
        thumbnailData, err := io.ReadAll(multipartFile)

        thumbnailVideo, err := cfg.db.GetVideo(videoID)
        if err != nil {
            respondWithError(w, http.StatusNotFound, "Error while fetching video; video not found:", err)
            return
        }

    thumbnailString := base64.StdEncoding.EncodeToString(thumbnailData)
    thumbnailInfo := fmt.Sprintf("data:%s;base64,%s", mediaType, thumbnailString)
    thumbnailVideo.ThumbnailURL = &thumbnailInfo
    
    err = cfg.db.UpdateVideo(thumbnailVideo)
    if err != nil {
        respondWithError(w, http.StatusInternalServerError, "Error while updating video info to database:", err)
        return
    }

    respondWithJSON(w, http.StatusOK, thumbnailVideo)
}

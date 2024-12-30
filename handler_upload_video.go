package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	//	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/database"
	"github.com/google/uuid"
	"io"
	"mime"
	"net/http"
	"os"
	"os/exec"
	// "strings"
	// "time"
)

const lRatio = 16.0 / 9.0
const pRatio = 9.0 / 16.0
const margin = 0.0025

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	const maxMemory = 1 << 30
	r.Body = http.MaxBytesReader(w, r.Body, maxMemory)

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

	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "Error while fetching video; video not found:", err)
		return
	}

	if video.CreateVideoParams.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "User does not own video!:", err)
		return
	}

	multipartFile, multipartFileHeader, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error while reading form:", err)
		return
	}
	defer multipartFile.Close()

	mediaType, _, err := mime.ParseMediaType(multipartFileHeader.Header.Get("Content-Type"))
	if mediaType != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "Invalid content type", err)
		return
	}
	videoFile, err := os.CreateTemp("", "tubely-*.mp4")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error while creating temp directory:", err)
		return
	}

	defer os.Remove(videoFile.Name())
	defer videoFile.Close()
	_, err = io.Copy(videoFile, multipartFile)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error while copying video from wire to temp file:", err)
		return
	}
	videoFile.Seek(0, io.SeekStart)

	videoProcessed, err := processVideoForFastStart(videoFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error while processing video:", err)
		return
	}

	videoFileProcessed, err := os.Open(videoProcessed)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error while processing video:", err)
		return
	}
	defer os.Remove(videoFileProcessed.Name())
	defer videoFileProcessed.Close()

	//Get video aspect ratio to organize s3Bucket files
	ratio, err := getVideoAspectRatio(videoFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error while getting video aspect ratio", err)
		return
	}

	// Get random video key
	b := make([]byte, 32)
	_, err = rand.Read(b)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error while getting random bytes", err)
		return
	}
	randomBytes := fmt.Sprintf("%s/%s.mp4", ratio, hex.EncodeToString(b))

	// Upload video to aws s3
	s3Input := &s3.PutObjectInput{Bucket: aws.String(cfg.s3Bucket), Key: aws.String(randomBytes), Body: videoFileProcessed, ContentType: aws.String(mediaType)}
	_, err = cfg.s3Client.PutObject(context.TODO(), s3Input)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error while uploading video to aws s3:", err)
		return
	}

	// Construct Video URL
	videoURL := fmt.Sprintf("%s/%s", cfg.s3CfDistribution, randomBytes)
	//videoURL := fmt.Sprintf("%s,%s", cfg.s3Bucket, randomBytes)
	video.VideoURL = &videoURL

	// Update video in database
	err = cfg.db.UpdateVideo(video)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error while updating video info to database:", err)
		return
	}

	//	signedVideo, err := cfg.dbVideoToSignedVideo(video)
	//	if err != nil {
	//		respondWithError(w, http.StatusInternalServerError, "Error while creating signed URL!", err)
	//		return
	//	}

	respondWithJSON(w, http.StatusOK, video)

}

func getVideoAspectRatio(filePath string) (string, error) {
	cmd := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filePath)
	var out bytes.Buffer
	cmd.Stdout = &out

	err := cmd.Run()
	if err != nil {
		return "", err
	}
	type VideoAspectRatio struct {
		Streams []struct {
			Width  int `json:"width,omitempty"`
			Height int `json:"height,omitempty"`
		} `json:"streams"`
	}

	var videoAspectRatio VideoAspectRatio
	err = json.Unmarshal(out.Bytes(), &videoAspectRatio)
	if err != nil {
		return "", err
	}

	aspect := float64(videoAspectRatio.Streams[0].Width) / float64(videoAspectRatio.Streams[0].Height)
	if aspect >= (pRatio-margin) && aspect <= (pRatio+margin) {
		return "portrait", nil
	} else if aspect >= (lRatio-margin) && aspect <= (lRatio+margin) {
		return "landscape", nil
	} else {
		return "other", nil
	}

}

func processVideoForFastStart(filePath string) (string, error) {
	filePathProcessed := fmt.Sprintf("%s.processing", filePath)

	cmd := exec.Command("ffmpeg", "-i", filePath, "-c", "copy", "-movflags", "faststart", "-f", "mp4", filePathProcessed)
	err := cmd.Run()
	if err != nil {
		return "", err
	}

	return filePathProcessed, nil
}

//func generatePresignedURL(s3Client *s3.Client, bucket, key string, expireTime time.Duration) (string, error) {
//	presignClient := s3.NewPresignClient(s3Client)
//	s3Input := &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)}
//
//	presignReq, err := presignClient.PresignGetObject(context.TODO(), s3Input, s3.WithPresignExpires(expireTime))
//	if err != nil {
//		return "", err
//	}
//
//	return presignReq.URL, nil
//}
//
//func (cfg *apiConfig) dbVideoToSignedVideo(video database.Video) (database.Video, error) {
//	if video.VideoURL == nil {
//		return video, nil
//	}
//	bucketKey := strings.Split(*video.VideoURL, ",")
//	presignedUrl, err := generatePresignedURL(cfg.s3Client, bucketKey[0], bucketKey[1], 5*time.Second)
//	if err != nil {
//		return database.Video{}, err
//	}
//	video.VideoURL = &presignedUrl
//	return video, nil
//}

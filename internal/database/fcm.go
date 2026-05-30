package database

import (
	"context"
	"log"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/messaging"
	"google.golang.org/api/option"
)

type NotificationEngine struct {
	MsgClient *messaging.Client
}

// InitFCM initializes the Firebase Cloud Messaging client.
// It falls back to a mocked mode if serviceAccountPath is empty or invalid.
func InitFCM(serviceAccountPath string) (*NotificationEngine, error) {
	if serviceAccountPath == "" {
		log.Println("WARNING: FIREBASE_CREDENTIALS_PATH is empty. Push notification system will run in [MOCKED] mode.")
		return &NotificationEngine{MsgClient: nil}, nil
	}

	ctx := context.Background()
	opt := option.WithCredentialsFile(serviceAccountPath)

	app, err := firebase.NewApp(ctx, nil, opt)
	if err != nil {
		log.Printf("WARNING: Failed to initialize Firebase App: %v. Falling back to [MOCKED] mode.", err)
		return &NotificationEngine{MsgClient: nil}, nil
	}

	msgClient, err := app.Messaging(ctx)
	if err != nil {
		log.Printf("WARNING: Failed to initialize FCM messaging client: %v. Falling back to [MOCKED] mode.", err)
		return &NotificationEngine{MsgClient: nil}, nil
	}

	log.Printf("FCM Notification Engine successfully initialized with credentials from %s.", serviceAccountPath)
	return &NotificationEngine{MsgClient: msgClient}, nil
}

// SendPushNotification dispatches a push notification to a target device token.
func (ne *NotificationEngine) SendPushNotification(ctx context.Context, deviceToken, title, body string) error {
	if deviceToken == "" {
		return nil // skip if token is empty
	}

	if ne.MsgClient == nil {
		log.Printf("[MOCK FCM PUSH] Target: %s | Title: %s | Body: %s", deviceToken, title, body)
		return nil
	}

	message := &messaging.Message{
		Token: deviceToken,
		Notification: &messaging.Notification{
			Title: title,
			Body:  body,
		},
		Android: &messaging.AndroidConfig{
			Priority: "high",
		},
		APNS: &messaging.APNSConfig{
			Headers: map[string]string{
				"apns-priority": "10",
			},
		},
	}

	_, err := ne.MsgClient.Send(ctx, message)
	if err != nil {
		log.Printf("Error sending FCM notification: %v", err)
		return err
	}

	return nil
}

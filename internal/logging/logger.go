package logging

import (
	"os"
	"time"

	"github.com/sirupsen/logrus"
)

var Log *logrus.Logger

func init() {
	Log = logrus.New()
	Log.SetFormatter(&logrus.JSONFormatter{
		TimestampFormat: time.RFC3339,
	})
	Log.SetOutput(os.Stdout)
	Log.SetLevel(logrus.InfoLevel)
}

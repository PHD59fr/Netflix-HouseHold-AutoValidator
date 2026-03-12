# 🏠 Netflix Household Auto-Validator

Automated Netflix household location verification through email monitoring and browser automation.

[![Go Version](https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Docker](https://img.shields.io/badge/Docker-Available-2496ED?style=flat&logo=docker)](https://hub.docker.com/r/phd59fr/netflix-household-autovalidator)

## 📝 Description

This application monitors an IMAP mailbox for emails from Netflix links. It is designed to automate the process of verifying the primary location for Netflix accounts.

**Key Features:**
- 📧 Automated IMAP email monitoring
- 🌐 Headless browser automation (Rod/Chromium)
- 🔐 Multi-account support with credentials
- 🧹 Automatic temp directory cleanup
- 📊 Structured JSON logging with trace IDs
- 🐳 Docker-ready

## 📂 Code Structure

```
cmd/
└── main.go                   # Entry point

internal/
├── models/                   # Domain types (Config, Email, BrowserResult)
├── config/                   # Configuration loading
├── logging/                  # JSON logger setup
├── imapfetch/               # IMAP client (interface + implementation)
├── mailparse/               # Email parsing & link extraction
├── emailprocessor/          # Email workflow orchestration
└── netflix/                 # Netflix service & browser automation
```

**Design Patterns:**
- Clean Architecture (domain models separated)
- Dependency Injection (interfaces for testability)
- Repository Pattern (IMAP client abstraction)

## ⚙️ Configuration
**Edit the `config.yaml` file at the root of the project with the following structure:**

```yaml
   email:
     imap: "imap.example.com:993"
     login: "your-email@example.com"
     password: "your-email-password"
     mailbox: "INBOX"
   targetFrom: "info@account.netflix.com"
   targetSubject: "Important : comment mettre à jour votre foyer Netflix"

   filterByAccount: false # if true, the application will only process emails that match the email addresses in the netflixAuth section
   netflixAuth:
     - email: "your-netflix-email@example.com" #Optional
       password: "your-netflix-password" #Optional
     - email: "your-netflix-email2@example.com" #Optional
       password: "your-netflix-password2" #Optional
  ```
**Note:** Make sure to replace the values with your own information.


**Configuration Notes:**
- `filterByAccount`: When `true`, matches email recipients against `netflixAuth` accounts
- `netflixAuth`: Optional credentials for automatic login if Netflix requires it

## 🚀 Usage

### Local Development

```bash
# Install dependencies
go mod download

# Run
go run ./cmd/main.go

# Build
go build -o validator ./cmd/main.go

# Run tests
go test ./...
```

### 🐳 Docker

```bash
# Pull image
docker pull phd59fr/netflix-household-autovalidator

# Run with volume-mounted config
docker run -v $(pwd)/config.yaml:/app/config.yaml phd59fr/netflix-household-autovalidator
```

**Docker Hub:** [phd59fr/netflix-household-autovalidator](https://hub.docker.com/r/phd59fr/netflix-household-autovalidator)

### Build from Source

```bash
# Build binary
go build -o validator ./cmd/main.go

# Run
./validator
```

## 📦 Project Structure

```
.
├── cmd/
│   └── main.go                      # Application entry point
├── internal/
│   ├── models/                      # Domain types
│   │   ├── config.go                # Config, EmailConfig, NetflixAccount
│   │   ├── email.go                 # Email struct
│   │   └── browser.go               # BrowserResult enum
│   ├── config/
│   │   └── config.go                # YAML config loader
│   ├── logging/
│   │   └── logger.go                # Logrus JSON logger
│   ├── imapfetch/
│   │   ├── imap.go                  # Client interface
│   │   └── client.go                # StandardClient implementation
│   ├── mailparse/
│   │   └── parser.go                # Email parsing & link extraction
│   ├── emailprocessor/
│   │   └── processor.go             # Email workflow orchestration
│   └── netflix/
│       ├── browser.go               # Browser interface
│       ├── browser_rod.go           # Rod browser implementation
│       └── service.go               # Netflix logic (filters, handlers)
├── config.yaml                      # Configuration file
├── Dockerfile                       # Docker build
└── go.mod                           # Go module definition
```

## 🔧 How It Works

1. **Monitoring**: Uses IMAP IDLE to subscribe for unseen emails from last 15 minutes
2. **Filtering**: Checks email sender (`targetFrom`) and subject (`targetSubject`)
3. **Parsing**: Extracts `update-primary-location` links from email body
4. **Automation**: Opens link in headless browser (Rod + Chromium)
   - Accepts cookie banners
   - Detects and fills login forms (if credentials provided)
   - Clicks confirmation button
   - Detects expired links
5. **Marking**: Marks email as read only if successfully handled
6. **Cleanup**: Hourly cleanup of temporary browser directories

## 🧪 Testing

```bash
# Run all tests
go test ./...

# Run with coverage
go test -cover ./...

# Run specific package tests
go test ./internal/config/
go test ./internal/mailparse/
go test ./internal/netflix/
```

**Test Coverage:**
- Configuration loading
- MIME header decoding
- Link extraction
- Email validation logic
- Netflix service filters
- Mock browser scenarios

## 📊 Logging

Structured JSON logs with trace IDs for correlation:

```json
{
  "level": "info",
  "msg": "Email received for user@example.com",
  "time": "2026-02-11T20:00:00Z",
  "trace_id": "550e8400-e29b-41d4-a716-446655440000"
}
```

**Trace ID**: Each email gets a unique UUID for tracking through the entire workflow.

## 📦 Dependencies

- **[Go IMAP](https://github.com/emersion/go-imap)** - IMAP client for Go.
- **[Rod](https://github.com/go-rod/rod)** - Browser automation tool.
- **[Logrus](https://github.com/sirupsen/logrus)** - Logging library.
- **[YAML.v2](https://gopkg.in/yaml.v2)** - YAML parsing library.
- **[go-message](https://github.com/emersion/go-message)** - Email parsing
- **[uuid](https://github.com/google/uuid)** - UUID generation

## 📄 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## 🍰 Contributing
Contributions are what make the open source community such an amazing place to be learn, inspire, and create. Any contributions you make are **greatly appreciated**.

## ❤️ Support
A simple star to this project repo is enough to keep me motivated on this project for days. If you find your self very much excited with this project let me know with a tweet.

If you have any questions, feel free to reach out to me on [X](https://twitter.com/xxPHDxx).
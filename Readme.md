# 🏠 Netflix Household Auto-Validator

Automated Netflix household location verification through email monitoring and browser automation.

[![Go Version](https://img.shields.io/badge/Go-1.26+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Docker](https://img.shields.io/badge/Docker-Available-2496ED?style=flat&logo=docker)](https://hub.docker.com/r/phd59fr/netflix-household-autovalidator)

## 📝 Description

This application monitors an IMAP mailbox for Netflix emails containing household verification links and automatically validates them. No manual action required.
**Key Features:**
- 📧 Monitors your mailbox for Netflix verification emails
- ⚡ Automatically validates household location from email links
- 🧠 Ignores expired or invalid links
- 📊 Structured JSON logs with trace IDs
- 🐳 Ready to run with Docker

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
```
**Note:** Make sure to replace the values with your own information.

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

### Environment Variables

| Variable       | Description      |
|----------------|------------------|
| EMAIL_IMAP     | IMAP server      |
| EMAIL_LOGIN    | Email login      |
| EMAIL_PASSWORD | Email password   |
| EMAIL_MAILBOX  | Mailbox name     |
| TARGET_FROM    | Expected sender  |
| TARGET_SUBJECT | Expected subject |

### 🐳 Docker

```bash
# Pull image
docker pull phd59fr/netflix-household-autovalidator

# Run with volume-mounted yaml config
docker run -v $(pwd)/config.yaml:/app/config.yaml phd59fr/netflix-household-autovalidator

# Run with environment variables (overrides config.yaml)
docker run \
  -e EMAIL_IMAP=imap.example.com:993 \
  -e EMAIL_LOGIN=your-email@example.com \
  -e EMAIL_PASSWORD=your-password \
  -e TARGET_FROM=info@account.netflix.com \
  -e TARGET_SUBJECT="Important : comment mettre à jour votre foyer Netflix" \
  phd59fr/netflix-household-autovalidator
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
│   └── main.go                  # Application entry point
├── internal/
│   ├── config/                  # Config loading
│   ├── emailprocessor/          # Email processing workflow
│   ├── imap/                    # IMAP client
│   ├── logging/                 # Structured JSON logger
│   ├── mailparse/               # Email parsing & link extraction
│   ├── models/                  # Domain models (Config, Email, BrowserResult)
│   └── netflix/                 # Netflix service & browser automation
├── config.yaml                  # Optional YAML configuration
├── Dockerfile                   # Container build
├── .github/workflows/           # CI/CD (Docker build & publish)
├── go.mod / go.sum              # Dependencies
└── LICENSE
```

## 🏗️ Architecture
- Event-driven flow based on IMAP IDLE (reacts to incoming emails)
- Pipeline processing: fetch → parse → filter → handle → mark as seen
- Clear separation between:
   - domain (`models`)
   - infrastructure (`imap`, `netflix`, `logging`)
   - orchestration (`emailprocessor`)
- Dependency injection via interfaces:
   - `imap.Client` for email access
   - `netflix.Browser` for browser automation
- Service layer (`netflix.Service`) encapsulating business logic

## 🔧 How It Works

1. **Monitoring**: Uses IMAP IDLE to subscribe for unseen emails from last 15 minutes
2. **Filtering**: Checks email sender (`targetFrom`) and subject (`targetSubject`)
3. **Parsing**: Extracts `update-primary-location` links from email body
4. **Automation**: Opens the validation link in a headless browser and completes the confirmation
   - Accepts cookie banners
   - Detects login requirement and aborts if authentication is needed
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
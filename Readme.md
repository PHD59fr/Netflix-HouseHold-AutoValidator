# Netflix HouseHold Auto-Validator

## 📝 Description

This application monitors an IMAP mailbox for emails from Netflix links. It is designed to automate the process of verifying the primary location for Netflix accounts.

## ⚙️ Configuration
**Edit the `config.yaml` file at the root of the project with the following structure:**

   ```yaml
   email:
     imap: "imap.example.com:993"
     login: "your-email@example.com"
     password: "your-email-password"
     refreshTime: 20s
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

## 🚀 Usage

To start the application, run \o/:

```sh
go run main.go
```

The application will connect to the specified mailbox in the configuration file, search for unread emails with the specified subject, and attempt to open Netflix verification links contained in these emails.

## 🐳 Docker
Find the Docker image to pull: [Netflix Household Auto-Validator Docker Image](https://hub.docker.com/r/phd59fr/netflix-household-autovalidator)

## 📂 Code Structure

- `main.go`: Main entry point of the application. Loads the configuration and starts the main loop.
- `email.go`: Contains functions to connect to the mailbox, search for, and process emails.
- `browser.go`: Contains functions to open Netflix verification links in a Rod-controlled browser.
- `utils.go`: Contains utility functions such as extracting links and MIME decoding.

## 📦 Dependencies

- [Go IMAP](https://github.com/emersion/go-imap): IMAP client for Go.
- [Rod](https://github.com/go-rod/rod): Browser automation tool.
- [Logrus](https://github.com/sirupsen/logrus): Logging library.
- [YAML.v2](https://gopkg.in/yaml.v2): YAML parsing library.

## 🍰 Contributing
Contributions are what make the open source community such an amazing place to be learn, inspire, and create. Any contributions you make are **greatly appreciated**.

## ❤️ Support
A simple star to this project repo is enough to keep me motivated on this project for days. If you find your self very much excited with this project let me know with a tweet.

If you have any questions, feel free to reach out to me on [Twitter](https://twitter.com/xxPHDxx).

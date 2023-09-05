# Communautofinder Telegram Bot

## Goal

The goal of this Telegram bot is to provide an interface for launching a Communauto car search on Telegram. When it finds at least one car, the bot will return the number of cars found.

## Commands

- **/help**: View available commands.
- **/start**: Begin a discussion with the bot to set search parameters.
- **/restart**: Start a new search with the same parameters as the previous one.

## Usage

To use this bot, you need to define an environment variable named *TOKEN_COMMUNAUTOSEARCH_BOT* and set its value to your Telegram token.  
After that, run ```go run main.go``` to launch the bot.  
Then, search for the bot on Telegram to start a discussion with it.

## Dependencies

- [communautofinder](https://github.com/craftlion/communautofinder)
- [telegram-bot-api](https://github.com/go-telegram-bot-api/telegram-bot-api)
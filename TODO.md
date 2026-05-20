# TODO

## UI Bugs
- [x] Command box do not display well multiline commands (breaks the background)

## Easy to implement
- [x] Implement a "plan" mode that only shows how something should be done
- [x] Add an option to ask for confirmation before executing an entire plan
- [ ] Add more response style options (more or less human, more or less technical, etc.)
- [x] Add an option to show the full plan before starting, or only a summary as it works now
- [ ] Add an option to show or hide the commands that will be executed; if hidden, show only the description
- [x] Review and unify the system for handling “yes/no”-type questions, making it more reusable and avoiding so much duplicated code. 

## LLM
- [x] Better integration with other LLM than OpenAI GPT
- [x] Allow the user to choose the model to use
- [ ] The way the system detects when the user is asking to repeat the last action or something similar needs to be improved. Right now it relies on hardcoded keyword detection. It might make more sense to perform a preliminary evaluation of the user’s intent to determine whether they are asking to repeat or re-execute a previous action. At the moment it only supports English and Catalan, and it is not working correctly. 

## Difficult
- [ ] Implement tmux support
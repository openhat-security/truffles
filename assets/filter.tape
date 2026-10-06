# truffles — glob filter (*run*) on openhat-security.
#
#   make demo-vhs TAPE=filter
#   vhs assets/filter.tape

Output assets/screenshots/filter.gif
Output assets/screenshots/filter.mp4

Require go
Require ffmpeg

Set Shell "bash"
Set FontSize 15
Set Width 1280
Set Height 800
Set TypingSpeed 36ms
Set Theme "Catppuccin Mocha"
Set Padding 16
Set BorderRadius 8
Set WindowBar Colorful
Set Framerate 30

Hide
Type "unset NO_COLOR FORCE_COLOR; export TERM=xterm-256color CLICOLOR=1"
Enter
Sleep 200ms
Type "test -x ./truffles || go build -o truffles ./cmd/truffles"
Enter
Sleep 2s
Type "clear"
Enter
Sleep 400ms
Show

Type "./truffles search -no-proxy -owner openhat-security '*run*' -out repos-run.txt"
Enter
Sleep 5s

Type "cat repos-run.txt"
Enter
Sleep 2s

Type "echo '✓ glob filter *run* matched'"
Enter
Sleep 2s

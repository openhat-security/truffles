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
Set TypingSpeed 42ms
Set Theme "Catppuccin Mocha"
Set Padding 16
Set BorderRadius 8
Set WindowBar Colorful
Set Framerate 30

Hide
Type "unset NO_COLOR FORCE_COLOR; export TERM=xterm-256color CLICOLOR=1"
Enter
Sleep 300ms
Type "test -x ./truffles || go build -o truffles ./cmd/truffles"
Enter
Sleep 2s
Type "rm -f repos-run.txt"
Enter
Sleep 400ms
Type "clear"
Enter
Sleep 500ms
Show

Type "./truffles search -no-proxy -owner openhat-security '*run*' -out repos-run.txt"
Sleep 2s
Enter
Sleep 7s

Type "cat repos-run.txt"
Sleep 1.5s
Enter
Sleep 4s

Type "echo '✓ glob filter *run* matched'"
Sleep 1.2s
Enter
Sleep 3.5s

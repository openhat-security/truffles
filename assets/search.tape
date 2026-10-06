# truffles — full owner enumeration of openhat-security.
#
#   make demo-vhs TAPE=search
#   vhs assets/search.tape

Output assets/screenshots/search.gif
Output assets/screenshots/search.mp4

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

Type "./truffles"
Enter
Sleep 2s

Type "./truffles search -no-proxy -owner openhat-security -out repos-ohs.txt"
Enter
Sleep 6s

Type "wc -l repos-ohs.txt && head -8 repos-ohs.txt"
Enter
Sleep 2.5s

Type "echo '✓ openhat-security org search complete'"
Enter
Sleep 2s

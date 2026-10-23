Lambda Prompt (also known as lambda-prompt) is
a prompt for Bash that gives basic Git and JJ
statuses, as well as condenses down path lengths.

To use, build the binary like so:

    go build -ldflags "-s -w" -o lambda-prompt .

    (Optional for compression)
    upx lambda-prompt

and put it somewhere in PATH. Then in your .bashrc

    eval "$(lambda-prompt init)"

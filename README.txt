Lambda Prompt (also known as lambda-prompt) is
a propmpt for Bash that gives basic Git and
JJ status, as well as condenses down path lengths.

To use, build the binary like so:

    go build -ldflags "-s -w" -o lambda-prompt .

    (Optional for compression)
    upx lambda-prompt

and put it somewhere in PATH. Then in your .bashrc

    eval "$(lambda-prompt init)"

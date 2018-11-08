# livechat-to-s3
Go script to export all chats in LiveChat API (https://developers.livechatinc.com/docs/rest-api/#archives) to AWS S3

# Before Running

export LIVECHAT_API_KEY=\<\<PASTE HERE YOUR LIVECHAT API KEY\>\> LIVECHAT_LOGIN=\<\<PASTE HERE YOUR LIVECHAT LOGIN\>\> AWS_PROFILE=\<\<PASTE HERE YOUR PROFILE (~/.aws/credentials)\>\>

# Result

This script will save a local copy of the files and save them on aws as well.

./files/originals/CHAT_ID.json --> The raw json given by API\
./files/transcript/your-customer@email.com/2018-10-29 1533.txt --> The transcription of an specific chat categorized by your customer's email

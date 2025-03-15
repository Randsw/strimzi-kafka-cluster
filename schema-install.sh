echo '{
  "type": "object",
  "properties": {
    "user": {
      "type": "string"
    },
    "car": {
      "type": "string"
    },
    "color": {
      "type": "string"
    }
  }
}' | \
jq '. | {schema: tojson, schemaType: "JSON"}' | \
curl -X POST "https://localhost:8081/subjects/example-topic/versions" -H "Content-Type:application/json"  -d @- 
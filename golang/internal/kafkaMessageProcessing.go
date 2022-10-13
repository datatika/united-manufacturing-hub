package internal

import (
	"bytes"
	"encoding/gob"
	"github.com/confluentinc/confluent-kafka-go/kafka"
	"github.com/coocood/freecache"
	jsoniter "github.com/json-iterator/go"
	"go.uber.org/zap"
)

// ParsedMessage is a struct that contains the parsed message key and value as AssetId, Location, CustomerId, PayloadType & Payload
type ParsedMessage struct {
	Payload          []byte
	TopicInformation TopicInformationV1
}

type TopicProbeMessage struct {
	Topic string `json:"topic"`
}

// ParseMessage parses a kafka message and returns a ParsedMessage struct or false if the message is not a valid message
func ParseMessage(msg *kafka.Message) (bool, ParsedMessage) {

	valid, found, message := GetCacheParsedMessage(msg)
	if !valid {
		return false, ParsedMessage{}
	}
	if found {
		return true, message
	}

	valid, m := PutCacheKafkaMessageAsParsedMessage(msg)
	if !valid {
		return false, ParsedMessage{}
	}

	return true, m

}

// Messagecache is a cache of messages, this prevents double parsing of messages
var Messagecache *freecache.Cache

func InitMessageCache(messageCacheSizeBytes int) {
	Messagecache = freecache.NewCache(messageCacheSizeBytes)
}

// PutCacheKafkaMessageAsParsedMessage tries to parse the kafka message and put it into the message cache, returning the parsed message if successful
func PutCacheKafkaMessageAsParsedMessage(msg *kafka.Message) (valid bool, message ParsedMessage) {
	valid = false
	if msg == nil || msg.TopicPartition.Topic == nil {
		return
	}
	topicInformation, err := GetTopicInformationV1Cached(*msg.TopicPartition.Topic)
	if err != nil {
		zap.S().Errorf("Failed to get topic information for topic: %s", *msg.TopicPartition.Topic)
		return false, ParsedMessage{}
	}
	if topicInformation == nil {
		zap.S().Errorf(" Invalid topic: %s", *msg.TopicPartition.Topic)
		return false, ParsedMessage{}
	}

	payload := msg.Value
	pm := ParsedMessage{
		Payload:          payload,
		TopicInformation: *topicInformation,
	}

	var cacheKey = AsXXHash(msg.Key, msg.Value, []byte((*msg.TopicPartition.Topic)))

	var buffer bytes.Buffer
	err = gob.NewEncoder(&buffer).Encode(pm)
	if err != nil {
		zap.S().Errorf("Failed to encode message: %s", err)
	} else {
		err = Messagecache.Set(cacheKey, buffer.Bytes(), 0)
		if err != nil {
			zap.S().Debugf("Error putting message in cache: %s", err)
		}
	}

	return true, pm
}

// GetCacheParsedMessage looks up the message cache for the key and returns the parsed message if found
func GetCacheParsedMessage(msg *kafka.Message) (valid bool, found bool, message ParsedMessage) {
	if msg == nil || msg.TopicPartition.Topic == nil {
		return false, false, ParsedMessage{}
	}

	var cacheKey = AsXXHash(msg.Key, msg.Value, []byte((*msg.TopicPartition.Topic)))
	get, err := Messagecache.Get(cacheKey)
	if err != nil {
		return true, false, ParsedMessage{}
	}

	var pm ParsedMessage
	reader := bytes.NewReader(get)
	err = gob.NewDecoder(reader).Decode(&pm)
	if err != nil {
		return false, true, ParsedMessage{}
	}

	return true, true, pm
}

// StartTopicProbeQueueProcessor processes the messages from the topic probe queue and triggers
// the refresh of the metadata for the consumers to discover the new created topic
func StartTopicProbeQueueProcessor(topicProbeProcessorChannel chan *kafka.Message) {
	zap.S().Debugf("[TP] Starting queue processor")
	for !ShuttingDownKafka {
		msg := <-topicProbeProcessorChannel
		if msg == nil {
			continue
		}

		var topicProbeMessage TopicProbeMessage
		err := jsoniter.Unmarshal(msg.Value, &topicProbeMessage)
		if err != nil {
			zap.S().Errorf("[TP] Failed to unmarshal topic probe message: %s", err)
			continue
		}

		if topicProbeMessage.Topic == "" {
			zap.S().Errorf("[TP] Empty topic in topic probe message")
			continue
		}

		if KafkaTopicProbeConsumer == nil {
			zap.S().Errorf("[TP] KafkaTopicProbeConsumer is nil")
			continue
		}

		_, err = KafkaTopicProbeConsumer.GetMetadata(&topicProbeMessage.Topic, false, 1000)
		if err != nil {
			zap.S().Errorf("[TP] Failed to get metadata for topic: %s", topicProbeMessage.Topic)
		}
	}
}

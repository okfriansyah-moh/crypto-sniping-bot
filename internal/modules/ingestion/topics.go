package ingestion

import "strings"

// Topic hash constants for Uniswap V2 events.
// Computed as keccak256(event_signature) — hardcoded because they are protocol
// constants that never change. All values are lowercase 0x-prefixed.
const (
	// TopicPairCreated is keccak256("PairCreated(address,address,address,uint256)")
	TopicPairCreated = "0x0d3648bd0f6ba80134a33ba9275ac585d9d315f0ad8355cddefde31afa28d0e9"

	// TopicMint is keccak256("Mint(address,uint256,uint256)")
	TopicMint = "0x4c209b5fc8ad50758f13e2e1088ba56a560dff690a1c6fef26394f4c03821c4f"

	// TopicSwap is keccak256("Swap(address,uint256,uint256,uint256,uint256,address)")
	TopicSwap = "0xd78ad95fa46c994b6551d0da85fc275fe613ce37657fb8d5e3d130840159d822"

	// TopicBurn is keccak256("Burn(address,uint256,uint256,address)")
	TopicBurn = "0xdccd412f0b1252819cb1fd330b93224ca42612892bb3f4f789976e6d81936496"
)

// knownTopics maps topic hash → human-readable event name for fast look-up.
var knownTopics = map[string]string{
	TopicPairCreated: "PairCreated",
	TopicMint:        "Mint",
	TopicSwap:        "Swap",
	TopicBurn:        "Burn",
}

// IsKnownTopic returns true if topic is one of the four tracked V2 events.
func IsKnownTopic(topic string) bool {
	_, ok := knownTopics[strings.ToLower(topic)]
	return ok
}

// TopicToEventName returns the human-readable name for a topic hash, or "Unknown".
func TopicToEventName(topic string) string {
	if name, ok := knownTopics[strings.ToLower(topic)]; ok {
		return name
	}
	return "Unknown"
}

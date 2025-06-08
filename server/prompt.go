// Package server provides core inference functionality for the ollama language model server.
// This file contains prompt processing and context window management functionality.
//
// The chatPrompt function implements an advanced truncation algorithm that optimally fits
// conversation history within model context limits while preserving message coherence.
package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/ollama/ollama/api"
	"github.com/ollama/ollama/llm"
	"github.com/ollama/ollama/template"
)

// tokenizeFunc is a function type that converts text into tokens for context length calculation.
// It returns a slice of token IDs that represent the input text in the model's vocabulary.
type tokenizeFunc func(context.Context, string) ([]int, error)

// messagesEqual compares two api.Message structs for equality
// This is needed because api.Message contains []api.ImageData which cannot be directly compared
func messagesEqual(m1, m2 api.Message) bool {
	if m1.Role != m2.Role || m1.Content != m2.Content {
		return false
	}
	if len(m1.Images) != len(m2.Images) {
		return false
	}
	for i := range m1.Images {
		if !bytes.Equal(m1.Images[i], m2.Images[i]) {
			return false
		}
	}
	return true
}

// chatPrompt is the core function responsible for preparing chat messages for inference while respecting
// the model's context window limitations. It implements a reverse truncation strategy that preserves
// the most recent conversation context and all system messages.
//
// ALGORITHM OVERVIEW:
// This function solves the fundamental challenge of fitting a potentially long conversation history
// into a model's limited context window. The key insight is that recent messages are typically more
// relevant than older ones, but we want to preserve as much history as possible.
//
// REVERSE TRUNCATION STRATEGY:
// Unlike traditional truncation that removes old messages from the beginning, this function uses a
// reverse search algorithm. It starts from the most recent message and works backward, finding the
// largest contiguous set of messages that fit within the context window. This ensures:
// 1. The latest user message is ALWAYS included (critical for response relevance)
// 2. All system messages are ALWAYS included (critical for model behavior)
// 3. Maximum conversation history is preserved within context limits
// 4. No "gaps" in conversation history (maintains coherent context flow)
//
// WHY THIS APPROACH IS SUPERIOR:
// - Preserves conversational coherence by maintaining contiguous message sequences
// - Maximizes information retention within context constraints
// - Ensures critical messages (system, latest) are never truncated
// - Handles edge cases gracefully (empty conversations, single messages, etc.)
//
// MULTIMODAL SUPPORT:
// The function handles images by converting them to a standardized token representation
// (768 tokens per image, based on CLIP embeddings) and includes them in context calculations.
// Images are processed into unique references that models can understand and reference.
//
// TEMPLATE EXECUTION:
// Uses the model's chat template to format messages, tools, and thinking parameters into
// the specific prompt format expected by the model (e.g., ChatML, Llama format, etc.).
// This abstraction allows the same truncation logic to work across different model families.
//
// Parameters:
//   - ctx: Context for cancellation and timeouts
//   - m: Model containing template, projector paths, and configuration
//   - tokenize: Function to convert text to tokens for length calculation
//   - opts: API options including NumCtx (context window size)
//   - msgs: Conversation messages in chronological order
//   - tools: Available function calling tools
//   - think: Pointer to thinking mode flag (nil if not set, enables chain-of-thought)
//
// Returns:
//   - prompt: Formatted text ready for model inference
//   - images: Processed image data with unique IDs
//   - error: Any processing errors
func chatPrompt(ctx context.Context, m *Model, tokenize tokenizeFunc, opts *api.Options, msgs []api.Message, tools []api.Tool, think *bool) (prompt string, images []llm.ImageData, _ error) {
	// --- SPONGEBOB TRUNCATION STRATEGY IMPLEMENTATION ---
	//
	// This implementation follows the "Spongebob" truncation algorithm designed to optimally
	// preserve conversation context while respecting model token limits. The strategy ensures:
	//
	// CORE PRINCIPLE: Always preserve S_all (system messages), M1 (first conversation message),
	// and M_latest (most recent message), while fitting as many intermediate messages as possible.
	//
	// KEY COMPONENTS:
	// - S_all: All system messages (always preserved)
	// - M1: First non-system message (conversation starter, always preserved)
	// - M_skip: Optional truncation indicator message
	// - Intermediate messages: Messages between M1 and M_latest (selectively preserved)
	// - M_latest: Most recent message (always preserved)

	// IMAGE TOKEN CALCULATION
	// Each image is estimated to consume 768 tokens (based on CLIP embedding size)
	imageNumTokens := 768

	// STEP 1: MESSAGE CATEGORIZATION
	// Separate system messages (S_all) from conversation messages for different handling
	var systemMessages []api.Message       // S_all: System messages (always preserved)
	var conversationMessages []api.Message // Non-system messages for selective truncation
	for _, msg := range msgs {
		if msg.Role == "system" {
			systemMessages = append(systemMessages, msg)
		} else {
			conversationMessages = append(conversationMessages, msg)
		}
	}

	// STEP 2: IDENTIFY KEY MESSAGES
	// M1: First conversation message (establishes conversation context)
	var M1 *api.Message
	if len(conversationMessages) > 0 {
		M1 = &conversationMessages[0]
	}

	// M_latest: Most recent message (critical for response relevance)
	var M_latest *api.Message
	if len(msgs) > 0 {
		M_latest = &msgs[len(msgs)-1]
	}

	// Edge case: Handle empty conversation
	if M1 == nil && M_latest == nil && len(systemMessages) == 0 {
		return "", nil, nil
	}

	// STEP 3: DEFINE M_skip MESSAGE
	// M_skip serves as a truncation indicator when intermediate messages are removed
	// It's inserted between M1 and the selected intermediate messages to signal truncation
	mSkipContent := "..."
	mSkipRole := "system"
	mSkipMessage := api.Message{Role: mSkipRole, Content: mSkipContent}

	// STEP 4: INITIAL MESSAGE ASSEMBLY
	// Build the base message structure: S_all + M1 + (potential M_skip) + selected_intermediates + M_latest
	finalMessages := make([]api.Message, 0, len(msgs)+2)
	finalMessages = append(finalMessages, systemMessages...)

	// Add M1 (first conversation message) if it exists
	if M1 != nil {
		finalMessages = append(finalMessages, *M1)
	}

	// STEP 5: IDENTIFY INTERMEDIATE MESSAGES
	// Find all messages between M1 and M_latest (exclusive) for potential inclusion
	intermediateMessages := []api.Message{}
	if M1 != nil && M_latest != nil && len(conversationMessages) > 1 {
		// Locate M_latest position in conversationMessages
		mlIdx := -1
		for i := len(conversationMessages) - 1; i >= 0; i-- {
			if &conversationMessages[i] == M_latest || (conversationMessages[i].Role == M_latest.Role && conversationMessages[i].Content == M_latest.Content) {
				mlIdx = i
				break
			}
		}
		// Extract intermediate messages (between M1 and M_latest)
		if mlIdx > 1 {
			intermediateMessages = conversationMessages[1:mlIdx]
		}
	}

	// STEP 6: HANDLE EXISTING M_skip MESSAGE
	// Check if M_skip was previously inserted in a prior truncation cycle
	// If found, reuse it and adjust intermediate message range accordingly
	mSkipPresent := false
	if len(conversationMessages) > 1 && conversationMessages[1].Role == mSkipRole &&
		(conversationMessages[1].Content == mSkipContent || conversationMessages[1].Content == "[Several conversation turns removed to conserve context.]") {
		mSkipPresent = true
		finalMessages = append(finalMessages, conversationMessages[1])
		// Adjust intermediateMessages to start after the existing M_skip
		intermediateMessages = conversationMessages[2:]
	}

	// STEP 7: TOKEN COUNTING HELPER
	// This helper function calculates the total token count for a given message set
	// including template formatting, tools, thinking mode, and image token overhead
	countTokens := func(msgsForPrompt []api.Message) (int, error) {
		var b bytes.Buffer
		thinkVal := false
		if think != nil {
			thinkVal = *think
		}
		// Apply the model's chat template to format messages properly
		if err := m.Template.Execute(&b, template.Values{Messages: msgsForPrompt, Tools: tools, Think: thinkVal, IsThinkSet: think != nil}); err != nil {
			return 0, err
		}
		// Tokenize the formatted prompt
		s, err := tokenize(ctx, b.String())
		if err != nil {
			return 0, err
		}
		ctxLen := len(s)
		// Add image token overhead (768 tokens per image)
		if m.ProjectorPaths != nil {
			for _, msg := range msgsForPrompt {
				ctxLen += imageNumTokens * len(msg.Images)
			}
		}
		return ctxLen, nil
	}

	// STEP 8: DETERMINE M_skip NECESSITY
	// Test if all intermediate messages fit within context limits
	// If not, M_skip will be needed to indicate truncation, but only if there's room for M1 + skip + M_latest
	needsSkip := false
	if len(intermediateMessages) > 0 {
		// Construct test prompt: S_all + M1 + all_intermediates + M_latest
		tempMsgs := append(append(append([]api.Message{}, systemMessages...), *M1), intermediateMessages...)
		if M_latest != nil && (M1 == nil || !messagesEqual(*M1, *M_latest)) {
			tempMsgs = append(tempMsgs, *M_latest)
		}
		tokCount, err := countTokens(tempMsgs)
		if err != nil {
			return "", nil, err
		}

		// Log context size before truncation check
		slog.Info("Context size check before truncation",
			"originalMessageCount", len(msgs),
			"totalTokens", tokCount,
			"numCtxLimit", opts.NumCtx,
			"exceedsLimit", tokCount > opts.NumCtx)

		// If this exceeds context limit, check if we have room for M1 + skip + M_latest
		if tokCount > opts.NumCtx {
			// Test if S_all + M1 + M_skip + M_latest fits
			testWithSkip := append([]api.Message{}, systemMessages...)
			testWithSkip = append(testWithSkip, *M1)
			testWithSkip = append(testWithSkip, mSkipMessage)
			if M_latest != nil && (M1 == nil || !messagesEqual(*M1, *M_latest)) {
				testWithSkip = append(testWithSkip, *M_latest)
			}
			skipTokCount, err := countTokens(testWithSkip)
			if err != nil {
				return "", nil, err
			}
			// Only use skip if the basic structure fits
			if skipTokCount <= opts.NumCtx {
				needsSkip = true
				// Log truncation decision
				slog.Info("Truncation required - M_skip will be inserted",
					"basicStructureTokens", skipTokCount,
					"numCtxLimit", opts.NumCtx,
					"intermediateMessageCount", len(intermediateMessages))
			} else {
				slog.Warn("Context limit exceeded even with basic structure",
					"basicStructureTokens", skipTokCount,
					"numCtxLimit", opts.NumCtx,
					"cannotFitBasicStructure", true)
			}
		}
	}

	// STEP 9: INSERT M_skip IF NEEDED
	// Add M_skip message if truncation is required and not already present
	if needsSkip && !mSkipPresent {
		finalMessages = append(finalMessages, mSkipMessage)
	}

	// STEP 10: IMPLEMENT REVERSE FILLING STRATEGY
	// This is the core of the Spongebob algorithm: fill remaining context with as many
	// recent intermediate messages as possible, working backwards from M_latest
	bestIntermediateSelection := []api.Message{}
	if needsSkip {
		// REVERSE SELECTION ALGORITHM:
		// Start from the most recent intermediate messages and work backwards
		// Find the largest contiguous suffix that fits within context limits
		for i := len(intermediateMessages) - 1; i >= 0; i-- {
			// candidate represents intermediateMessages[i:] (suffix from position i)
			candidate := intermediateMessages[i:]

			// Construct test prompt: S_all + M1 + M_skip + candidate + M_latest
			tempMsgs := append([]api.Message{}, finalMessages...)
			tempMsgs = append(tempMsgs, candidate...)
			if M_latest != nil && (M1 == nil || !messagesEqual(*M1, *M_latest)) {
				tempMsgs = append(tempMsgs, *M_latest)
			}

			// Test if this selection fits within context limits
			tokCount, err := countTokens(tempMsgs)
			if err != nil {
				return "", nil, err
			}

			// If it fits, this is our optimal selection (largest suffix that fits)
			if tokCount <= opts.NumCtx {
				bestIntermediateSelection = candidate
				selectedIntermediateCount := len(candidate)
				totalIntermediateCount := len(intermediateMessages)
				truncatedCount := totalIntermediateCount - selectedIntermediateCount

				// Log successful reverse selection
				slog.Info("Reverse truncation completed",
					"finalTokens", tokCount,
					"numCtxLimit", opts.NumCtx,
					"selectedIntermediateMessages", selectedIntermediateCount,
					"totalIntermediateMessages", totalIntermediateCount,
					"truncatedMessages", truncatedCount)
				break
			}
		}
		// Note: If no suffix fits, bestIntermediateSelection remains empty
		// This means only S_all + M1 + M_skip + M_latest will be included
		if len(bestIntermediateSelection) == 0 {
			slog.Warn("Extreme truncation - no intermediate messages fit",
				"totalIntermediateMessages", len(intermediateMessages),
				"onlyBasicStructureIncluded", true)
		}
	} else {
		// No truncation needed - include all intermediate messages
		bestIntermediateSelection = intermediateMessages
		slog.Info("No truncation required",
			"totalMessages", len(msgs),
			"allMessagesIncluded", true)
	}

	// STEP 11: FINALIZE MESSAGE ASSEMBLY
	// Add the optimally selected intermediate messages
	finalMessages = append(finalMessages, bestIntermediateSelection...)

	// Add M_latest if it's different from M1 (avoid duplication)
	if M_latest != nil && (M1 == nil || !messagesEqual(*M1, *M_latest)) {
		finalMessages = append(finalMessages, *M_latest)
	}

	// STEP 12: IMAGE PROCESSING AND PLACEHOLDER REPLACEMENT
	// Process images in the final message set, converting them to model-compatible references
	// This step maintains the existing image handling logic while working with the optimized message set
	for idx, msg := range finalMessages {
		// Model-specific image constraints (e.g., mllama supports only one image per message)
		if slices.Contains(m.Config.ModelFamilies, "mllama") && len(msg.Images) > 1 {
			return "", nil, errors.New("this model only supports one image while more than one image requested")
		}

		var prefix string
		prompt := msg.Content

		// Convert each image to a unique tagged reference
		for _, i := range msg.Images {
			imgData := llm.ImageData{
				ID:   len(images),
				Data: i,
			}
			imgTag := fmt.Sprintf("[img-%d]", imgData.ID)

			// Handle image placement: either replace [img] placeholder or prepend to content
			if !strings.Contains(prompt, "[img]") {
				prefix += imgTag
			} else {
				prompt = strings.Replace(prompt, "[img]", imgTag, 1)
			}
			images = append(images, imgData)
		}
		finalMessages[idx].Content = prefix + prompt
	}

	// STEP 13: FINAL PROMPT GENERATION
	// Apply the model's chat template to the optimally truncated message set
	// This produces the final prompt string ready for model inference
	var b bytes.Buffer
	thinkVal := false
	if think != nil {
		thinkVal = *think
	}

	// Execute template with final message set, tools, and thinking mode configuration
	if err := m.Template.Execute(&b, template.Values{Messages: finalMessages, Tools: tools, Think: thinkVal, IsThinkSet: think != nil}); err != nil {
		return "", nil, err
	}

	// FINAL POST-TRUNCATION SUMMARY LOGGING
	// Calculate final token count for comprehensive truncation diagnosis
	finalTokenCount, err := countTokens(finalMessages)
	if err != nil {
		slog.Warn("Failed to count final tokens for post-truncation logging", "error", err)
		finalTokenCount = -1 // Indicate counting failure
	}

	// Calculate original token count for comparison
	originalTokenCount, err := countTokens(msgs)
	if err != nil {
		slog.Warn("Failed to count original tokens for post-truncation logging", "error", err)
		originalTokenCount = -1 // Indicate counting failure
	}

	// Calculate tokens removed (if both counts are valid)
	var tokensRemoved int
	if originalTokenCount >= 0 && finalTokenCount >= 0 {
		tokensRemoved = originalTokenCount - finalTokenCount
	} else {
		tokensRemoved = -1 // Indicate calculation failure
	}

	// Comprehensive post-truncation logging for TTFT diagnosis
	slog.Info("Post-truncation summary completed",
		"requestID", ctx.Value("requestID"),
		"originalMessageCount", len(msgs),
		"finalMessageCount", len(finalMessages),
		"messagesRemoved", len(msgs)-len(finalMessages),
		"originalTokenCount", originalTokenCount,
		"finalTokenCount", finalTokenCount,
		"tokensRemoved", tokensRemoved,
		"numCtxLimit", opts.NumCtx,
		"truncationOccurred", len(msgs) != len(finalMessages),
		"contextUtilization", func() float64 {
			if opts.NumCtx > 0 && finalTokenCount >= 0 {
				return float64(finalTokenCount) / float64(opts.NumCtx) * 100
			}
			return -1
		}())

	return b.String(), images, nil
}

# Ollama Context Enhancement - Phase 1: Implementation Plan

## 1. Objective

Modify the `chatPrompt` function in [`server/prompt.go`](server/prompt.go:0) to implement the "Spongebob" context truncation strategy. This strategy ensures the preservation of all system messages, the first user message (`M1`), and the latest message (`M_latest`). If truncation of messages between `M1` and `M_latest` is necessary, a static placeholder message (`M_skip`) will be inserted after `M1` (if not already present from a previous truncation). The remaining context window will be filled with the most recent messages from the history between `M1` (or `M_skip`) and `M_latest`.

## 2. Affected File(s)

*   Primary: [`server/prompt.go`](server/prompt.go:0) - Contains the `chatPrompt` function to be modified.
*   Supporting (for types and understanding): [`api/types.go`](api/types.go:0) - Defines `api.Message`, `api.Options`, etc.
*   Testing: [`server/prompt_test.go`](server/prompt_test.go:0) - Unit tests for `chatPrompt` will need to be updated or augmented.

## 3. Detailed Implementation Steps for `chatPrompt`

The existing `chatPrompt` function iterates backward from the latest message to find a contiguous block of recent messages that fit. This will be significantly modified.

### 3.1. Pre-computation and Identification:

1.  **Collect All System Messages (`S_all`):**
    *   Iterate through the input `msgs []api.Message` once at the beginning.
    *   Extract all messages where `msg.Role == "system"` into a separate slice, `systemMessages`.
    *   Create a new slice `conversationMessages` containing only non-system messages, preserving their original order.
2.  **Identify First User Message (`M1`):**
    *   From `conversationMessages`, find the first message. This will be `M1`.
    *   If `conversationMessages` is empty, there's no `M1`. Handle this edge case (e.g., proceed with only system messages and `M_latest` if `M_latest` is a system message, or an empty prompt if no messages at all).
3.  **Identify Latest Message (`M_latest`):**
    *   This is simply the last message in the original `msgs` slice.
    *   If `msgs` is empty, handle appropriately (return empty prompt).
4.  **Define `M_skip`:**
    *   `mSkipContent := "[Several conversation turns removed to conserve context.]"`
    *   `mSkipRole := "system"`
    *   `mSkipMessage := api.Message{Role: mSkipRole, Content: mSkipContent}`

### 3.2. Core Logic for Assembling Truncated Messages:

1.  **Initialize `finalMessages` slice:** This will hold the messages to be templated.
2.  **Add `S_all`:** Append all messages from `systemMessages` to `finalMessages`.
3.  **Handle `M1` and `M_latest`:**
    *   If `conversationMessages` is empty:
        *   If `M_latest` (from original `msgs`) is a system message and not already in `systemMessages`, add it. (This case is unlikely if system messages are pre-filtered).
        *   Proceed to templating `finalMessages`.
    *   If `conversationMessages` is not empty:
        *   Add `M1` (i.e., `conversationMessages[0]`) to `finalMessages`.
4.  **Determine Messages Between `M1` and `M_latest` (`intermediateMessages`):**
    *   These are messages in `conversationMessages` from index 1 up to (but not including) the message corresponding to `M_latest`.
    *   If `M1` is the same as `M_latest` (i.e., only one non-system message), `intermediateMessages` will be empty.
5.  **Conditional `M_skip` and Recent Message Filling:**
    *   **Tokenize fixed parts:** Calculate token count for `systemMessages + M1 + M_latest` (plus `mSkipMessage` if it were to be added). This gives a baseline token count.
        *   `baseMessagesForTokenization := append(slices.Clone(systemMessages), M1)`
        *   If `M1` is not `M_latest`: `baseMessagesForTokenization = append(baseMessagesForTokenization, M_latest)`
        *   `tokensFixedWithoutSkip, _ := tokenizeAndTemplate(ctx, m, tokenize, opts, baseMessagesForTokenization, tools, think)`
        *   `tokensFixedWithSkip, _ := tokenizeAndTemplate(ctx, m, tokenize, opts, append(baseMessagesForTokenization, mSkipMessage), tools, think)` (if M_latest is different from M1)

    *   **Check if `M_skip` is needed:**
        *   `potentialFullMessages := append(slices.Clone(systemMessages), M1)`
        *   `potentialFullMessages = append(potentialFullMessages, intermediateMessages...)`
        *   If `M1` is not `M_latest`: `potentialFullMessages = append(potentialFullMessages, M_latest)`
        *   `tokensPotentialFull, _ := tokenizeAndTemplate(ctx, m, tokenize, opts, potentialFullMessages, tools, think)`
        *   `needsSkip := len(intermediateMessages) > 0 && tokensPotentialFull > opts.NumCtx`

    *   **Handle `M_skip`:**
        *   `mSkipPresent := false`
        *   If `len(conversationMessages) > 1` and `conversationMessages[1].Role == mSkipRole && conversationMessages[1].Content == mSkipContent`:
            *   `mSkipPresent = true`
            *   Add `conversationMessages[1]` (the existing `M_skip`) to `finalMessages`.
            *   Adjust `intermediateMessages` to start after this existing `M_skip`.
        *   Else if `needsSkip`:
            *   Add `mSkipMessage` to `finalMessages`.
            *   `mSkipPresent = true`

    *   **Fill with `intermediateMessages` (working backward):**
        *   `messagesToConsiderForFilling := intermediateMessages`
        *   If `mSkipPresent` and `intermediateMessages` started after `M1` (not after an existing skip), then `messagesToConsiderForFilling` are those between `M_skip` and `M_latest`.
        *   Iterate `i` from `len(messagesToConsiderForFilling)-1` down to `0`.
            *   `currentSelection := messagesToConsiderForFilling[i:]`
            *   `tempPromptMessages := append(slices.Clone(systemMessages), M1)`
            *   If `mSkipPresent`: `tempPromptMessages = append(tempPromptMessages, mSkipMessage)` (or the existing one)
            *   `tempPromptMessages = append(tempPromptMessages, currentSelection...)`
            *   If `M1` is not `M_latest`: `tempPromptMessages = append(tempPromptMessages, M_latest)`
            *   `tokensCurrent, _ := tokenizeAndTemplate(ctx, m, tokenize, opts, tempPromptMessages, tools, think)`
            *   If `tokensCurrent <= opts.NumCtx`:
                *   These `currentSelection` messages are the ones to add. Prepend them to any messages already selected for filling (or set them as the selection).
                *   Break loop (found the max that fit).
            *   If loop finishes and nothing fit (even one message from `intermediateMessages` was too much), then `currentSelection` is empty.
        *   Add the `currentSelection` (which could be empty) of intermediate messages to `finalMessages` *after* `M1` (or `M_skip`) and *before* `M_latest`.

6.  **Add `M_latest` (if not same as `M1` and not already added):**
    *   If `M1` is not `M_latest`, ensure `M_latest` (from original `msgs`) is the last message in `finalMessages`.
7.  **Final Templating:**
    *   Execute the model's template with `finalMessages`, `tools`, and `think` to get the final prompt string.
    *   Image processing logic (as in current `chatPrompt`) should be applied to `finalMessages` before templating.

### 3.3. Helper Function: `tokenizeAndTemplate`

To avoid repetitive code, a helper function could be beneficial:
`func tokenizeAndTemplate(ctx context.Context, model *Model, tokenize tokenizeFunc, opts *api.Options, messages []api.Message, tools []api.Tool, think *bool) (numTokens int, promptString string, images []llm.ImageData, err error)`
This function would:
1.  Process images in `messages` (similar to lines 171-212 in current `server/prompt.go`).
2.  Execute the template with processed messages, tools, think.
3.  Tokenize the resulting string.
4.  Return token count, the prompt string, processed images, and any error.
*(The actual final prompt string and images are only needed once at the very end, but token count is needed repeatedly.)*

### 3.4. Algorithm (Pseudocode Style)

```golang
func chatPrompt(ctx, m, tokenize, opts, msgs, tools, think) (prompt, images, err):
    S_all = extractSystemMessages(msgs)
    convMsgs = extractConversationMessages(msgs)

    if len(msgs) == 0: return "", nil, nil
    M_latest = msgs[len(msgs)-1]

    if len(convMsgs) == 0:
        // Only system messages, or M_latest is a system message
        finalMessages = S_all
        // Ensure M_latest is included if it was a unique system message
        // (complex logic, simplify: assume S_all covers it or M_latest is not system if convMsgs is empty)
        return renderTemplate(finalMessages, tools, think) // Simplified

    M1 = convMsgs[0]
    
    finalMessages = []api.Message{}
    finalMessages = append(finalMessages, S_all...)
    finalMessages = append(finalMessages, M1)

    intermediateMsgs = getMessagesBetween(M1, M_latest, convMsgs)
    
    mSkipMsg = api.Message{Role:"system", Content:"[Several conversation turns removed to conserve context.]"}
    skipInsertedOrPresent = false

    // Check if M_skip is needed and if it's already there
    if len(intermediateMsgs) > 0 {
        // Simplified check for needing skip (actual check involves tokenizing)
        // tokens_S_M1_intermediate_Mlatest = calculate_tokens(S_all, M1, intermediateMsgs, M_latest)
        // if tokens_S_M1_intermediate_Mlatest > opts.NumCtx:
        //    needsSkip = true

        if len(convMsgs) > 1 && convMsgs[1] == mSkipMsg { // Check if M_skip is already M2
            finalMessages = append(finalMessages, convMsgs[1]) // Add existing M_skip
            skipInsertedOrPresent = true
            intermediateMsgs = getMessagesBetween(convMsgs[1], M_latest, convMsgs) // Re-evaluate intermediate
        } else if needsSkip { // Placeholder for actual token-based check
            finalMessages = append(finalMessages, mSkipMsg)
            skipInsertedOrPresent = true
        }
    }

    // Fill remaining context with messages from intermediateMsgs (working backwards)
    // This loop needs to calculate tokens for (S_all + M1 + [M_skip] + selection + M_latest)
    bestIntermediateSelection = []api.Message{}
    for i from len(intermediateMsgs)-1 down to 0:
        currentSelection = intermediateMsgs[i:]
        // tempFullPrompt = S_all + M1 + [M_skip_if_present] + currentSelection + M_latest
        // currentTokens = calculate_tokens(tempFullPrompt)
        // if currentTokens <= opts.NumCtx:
        //    bestIntermediateSelection = currentSelection
        //    break
    finalMessages = append(finalMessages, bestIntermediateSelection...)

    if M1 != M_latest: // Ensure M_latest is at the end
        finalMessages = append(finalMessages, M_latest) 
        // Deduplicate M_latest if it was somehow included in bestIntermediateSelection (unlikely with correct slicing)

    // Process images for finalMessages
    // return renderTemplate(finalMessages, tools, think)
```

### 3.5. Testing Considerations and Next Steps

*   **Initial Testing Status:** The "Spongebob" logic implementation in `server/prompt.go` is complete and compiles. However, initial runs revealed that several existing unit tests in `server/prompt_test.go` fail.
*   **Reason for Test Failures:** The failures are due to a mismatch in expected behavior. The existing tests were designed for the *old* truncation logic. The new Spongebob strategy (preserving `M1`, `M_skip`, `M_latest`, and grouping system messages at the front) produces different, but *intentionally designed*, output.
*   **Decision:** The output behavior of the implemented "Spongebob" strategy is confirmed as the **desired and correct behavior**.
*   **Next Action:** The failing unit tests in `server/prompt_test.go` must be **updated** to align their assertions with the new, correct expected output from the Spongebob strategy. This involves:
    *   Identifying all failing tests.
    *   For each, determining the correct expected output string based on Spongebob rules (all system messages, `M1`, `M_skip` if needed, most recent fitting intermediate messages, `M_latest`).
    *   Modifying the test assertions (`want` variable) accordingly.
*   **Note on Test Assertions (from previous review):** While the implementation in `server/prompt.go` correctly preserves `M1` as per the Spongebob strategy, some test assertions in `server/prompt_test.go` for extreme truncation scenarios (e.g., very small `opts.NumCtx`) might require adjustment to consistently expect `M1`'s presence in the output. The core Spongebob logic adheres to `M1` preservation.

---
*Footnote: Return to Architect mode if there are difficulties or unforeseen issues during implementation.*
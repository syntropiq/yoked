#!/bin/bash

# Time to First Token Performance Assessment Script
# This script measures the performance impact of frequent loading/unloading
# by testing various request patterns with the existing client-side stopwatch

set -e

OLLAMA_HOST="${OLLAMA_HOST:-http://localhost:11434}"
MODEL="${1:-llama3.2:1b}"
TEST_RESULTS_DIR="./ttft_assessment_results"
TIMESTAMP=$(date +"%Y%m%d_%H%M%S")

echo "=== Time to First Token Performance Assessment ==="
echo "Model: $MODEL"
echo "Timestamp: $TIMESTAMP"
echo "Results will be saved to: $TEST_RESULTS_DIR"
echo

# Create results directory
mkdir -p "$TEST_RESULTS_DIR"

# Function to extract TTFT from ollama output
extract_ttft() {
    local output_file="$1"
    grep "Time to first token:" "$output_file" | tail -1 | sed 's/.*Time to first token: //' | sed 's/\r$//'
}

# Function to run a single test with specific parameters
run_test() {
    local test_name="$1"
    local prompt="$2"
    local num_ctx="$3"
    local iteration="$4"
    
    local output_file="$TEST_RESULTS_DIR/${test_name}_${iteration}_${TIMESTAMP}.log"
    local stderr_file="$TEST_RESULTS_DIR/${test_name}_${iteration}_${TIMESTAMP}.err"
    
    echo "Running $test_name (iteration $iteration)..."
    echo "  Prompt length: $(echo "$prompt" | wc -c) chars"
    echo "  NumCtx: $num_ctx"
    
    # Run ollama generate with verbose output and capture timing
    timeout 120s ollama generate "$MODEL" "$prompt" --verbose \
        $([ -n "$num_ctx" ] && echo "--num-ctx $num_ctx") \
        > "$output_file" 2> "$stderr_file" || {
        echo "  WARNING: Test timed out or failed"
        echo "timeout" > "$output_file"
        return 1
    }
    
    # Extract and report TTFT
    local ttft=$(extract_ttft "$stderr_file")
    if [ -n "$ttft" ]; then
        echo "  TTFT: $ttft"
        echo "$ttft" >> "$TEST_RESULTS_DIR/${test_name}_ttft.txt"
    else
        echo "  WARNING: Could not extract TTFT"
        echo "FAILED" >> "$TEST_RESULTS_DIR/${test_name}_ttft.txt"
    fi
    
    # Small delay between requests to avoid overwhelming the system
    sleep 2
}

# Test Case 1: Same Request Pattern (should benefit from caching if available)
echo "=== Test Case 1: Identical Requests (5 iterations) ==="
SAME_PROMPT="Explain the concept of machine learning in simple terms."
for i in {1..5}; do
    run_test "identical_requests" "$SAME_PROMPT" "4096" "$i"
done

echo

# Test Case 2: Different NumCtx Values (should trigger reloading)
echo "=== Test Case 2: Varying NumCtx Values ==="
VARYING_PROMPT="What is artificial intelligence?"
declare -a numctx_values=(2048 4096 8192 4096 2048)
for i in {1..5}; do
    run_test "varying_numctx" "$VARYING_PROMPT" "${numctx_values[$((i-1))]}" "$i"
done

echo

# Test Case 3: Different Prompt Lengths (should trigger dynamic sizing)
echo "=== Test Case 3: Varying Prompt Lengths ==="
declare -a prompt_lengths=(
    "Short prompt"
    "This is a medium length prompt that should require a different dynamic NumCtx calculation based on the message length calculation algorithm implemented in the system."
    "This is a very long prompt that is designed to test the dynamic NumCtx calculation with a much larger message length. The calculateDynamicNumCtx function should calculate a significantly larger context size based on the formula: max(4096, ((messageLength * 2 + 1023) / 1024) * 1024). This longer prompt will help us understand the performance impact when the system needs to load models with larger KV cache sizes. The prompt continues to grow in length to ensure we're testing edge cases where the dynamic context calculation results in substantially different NumCtx values, which should trigger model reloading if the previous model instance had a different KV cache size."
)

for i in {1..3}; do
    run_test "varying_length" "${prompt_lengths[$((i-1))]}" "" "$i"
done

echo

# Test Case 4: Mixed Pattern (realistic usage simulation)
echo "=== Test Case 4: Mixed Usage Pattern ==="
declare -a mixed_prompts=(
    "What is Python?"
    "Explain quantum computing in detail with examples and practical applications in modern technology."
    "Hello"
    "Write a comprehensive guide about machine learning algorithms including supervised learning, unsupervised learning, and reinforcement learning with practical examples."
    "Quick question: what is 2+2?"
)

for i in {1..5}; do
    run_test "mixed_pattern" "${mixed_prompts[$((i-1))]}" "" "$i"
done

echo

# Analyze Results
echo "=== Performance Analysis ==="

analyze_results() {
    local test_name="$1"
    local results_file="$TEST_RESULTS_DIR/${test_name}_ttft.txt"
    
    if [ ! -f "$results_file" ]; then
        echo "No results found for $test_name"
        return
    fi
    
    echo "Results for $test_name:"
    
    # Calculate statistics
    local valid_results=$(grep -v "FAILED\|timeout" "$results_file" | grep -E '^[0-9]')
    local count=$(echo "$valid_results" | wc -l)
    
    if [ "$count" -eq 0 ]; then
        echo "  No valid results"
        return
    fi
    
    # Convert to milliseconds for easier analysis
    local ms_results=$(echo "$valid_results" | sed 's/[a-zA-Z]//g' | awk '{
        if ($1 ~ /^[0-9]+\.[0-9]+s$/) print $1 * 1000
        else if ($1 ~ /^[0-9]+ms$/) print $1
        else if ($1 ~ /^[0-9]+\.[0-9]+ms$/) print $1
        else print $1
    }')
    
    local avg=$(echo "$ms_results" | awk '{sum+=$1} END {printf "%.1f", sum/NR}')
    local min=$(echo "$ms_results" | sort -n | head -1)
    local max=$(echo "$ms_results" | sort -n | tail -1)
    
    echo "  Count: $count"
    echo "  Average: ${avg}ms"
    echo "  Min: ${min}ms"
    echo "  Max: ${max}ms"
    echo "  Raw values: $(echo "$valid_results" | tr '\n' ' ')"
    echo
}

analyze_results "identical_requests"
analyze_results "varying_numctx"
analyze_results "varying_length"
analyze_results "mixed_pattern"

# Generate Summary Report
SUMMARY_FILE="$TEST_RESULTS_DIR/performance_summary_${TIMESTAMP}.txt"
echo "=== Performance Assessment Summary ===" > "$SUMMARY_FILE"
echo "Timestamp: $TIMESTAMP" >> "$SUMMARY_FILE"
echo "Model: $MODEL" >> "$SUMMARY_FILE"
echo "Ollama Host: $OLLAMA_HOST" >> "$SUMMARY_FILE"
echo >> "$SUMMARY_FILE"

echo "Test Results:" >> "$SUMMARY_FILE"
for test_name in "identical_requests" "varying_numctx" "varying_length" "mixed_pattern"; do
    echo "- $test_name:" >> "$SUMMARY_FILE"
    analyze_results "$test_name" >> "$SUMMARY_FILE"
done

echo "Assessment complete. Results saved to: $SUMMARY_FILE"
echo
echo "=== Key Findings ==="
echo "1. Compare 'identical_requests' vs other patterns to see reload overhead"
echo "2. 'varying_numctx' shows impact of different KV cache sizes"
echo "3. 'varying_length' shows dynamic NumCtx calculation overhead"
echo "4. 'mixed_pattern' simulates realistic usage"
echo
echo "Next steps:"
echo "1. Review the results to identify performance bottlenecks"
echo "2. Check server logs for MODEL_LOAD_START/MODEL_UNLOAD_COMPLETE events"
echo "3. Correlate TTFT spikes with model loading activity"
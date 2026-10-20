# A minimal agent in caller code

Republic supplies provider calls, messages and tool schemas. Your application
owns the loop, tool execution, permissions and stopping conditions.

1. Send user input and tool declarations with Chat.
2. Append response.Message to your conversation.
3. Validate each call's Args() and run the corresponding application function.
4. Append ToolResult(call, false, Text(result)); use true for failed tool execution.
5. Call Chat again with the conversation and the same declarations.
6. Stop when no tool calls remain or your application reaches its turn limit.

See [tool use](tools.md) for declarations and continuation messages. Keep the full
ToolCall and assistant Message so signatures and opaque provider blocks survive.
InMemoryHistory may retain conversation turns, but does not execute tools or loop.

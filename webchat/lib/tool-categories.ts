/**
 * Tool name constants and category router.
 *
 * Centralizes the mapping from AEP tool names to specialized GenUI components.
 * Import from here instead of using raw strings.
 */

// ── Tool name constants ──────────────────────────────────────
export const ToolName = {
  // Terminal
  RunCommand: "run_command",
  Bash: "bash",
  ExecuteCommand: "execute_command",
  Shell: "shell",
  // File
  EditFile: "edit_file",
  WriteFile: "write_file",
  ReplaceFileContent: "replace_file_content",
  CreateFile: "create_file",
  ApplyDiff: "apply_diff",
  Write: "write",
  WriteToFile: "write_to_file",
  MultiReplaceFileContent: "multi_replace_file_content",
  Edit: "edit",
  StrReplaceEditor: "str_replace_editor",
  // Search
  GrepSearch: "grep_search",
  ViewFile: "view_file",
  SearchFiles: "search_files",
  ReadFile: "read_file",
  ListDirectory: "list_directory",
  // Permission / Elicitation
  AskPermission: "ask_permission",
  Confirm: "confirm",
  Elicitation: "elicitation",
  // AI Tools
  SearchWeb: "search_web",
  GenerateImage: "generate_image",
  ReadUrlContent: "read_url_content",
} as const;

export type ToolCategory = "terminal" | "file" | "search" | "list" | "permission" | "todo" | "ai" | "default";

const TERMINAL_TOOLS: ReadonlySet<string> = new Set([
  ToolName.RunCommand, ToolName.Bash, ToolName.ExecuteCommand, ToolName.Shell,
]);

const TODO_TOOLS: ReadonlySet<string> = new Set([
  "todo", "todowrite", "todo_write", "task_list", "checklist"
]);

const FILE_TOOLS: ReadonlySet<string> = new Set([
  ToolName.EditFile, ToolName.WriteFile, ToolName.ReplaceFileContent,
  ToolName.CreateFile, ToolName.ApplyDiff,
  ToolName.Write, ToolName.WriteToFile, ToolName.MultiReplaceFileContent,
  ToolName.Edit, ToolName.StrReplaceEditor, "patch",
]);

const SEARCH_TOOLS: ReadonlySet<string> = new Set([
  ToolName.GrepSearch, ToolName.ViewFile, ToolName.SearchFiles,
  ToolName.ReadFile, "search_web", "read_url_content", "google_search",
]);

const LIST_TOOLS: ReadonlySet<string> = new Set([
  ToolName.ListDirectory, "ls",
]);

const AI_TOOLS: ReadonlySet<string> = new Set([
  "agent", "subagent", "ai_task", "neural_process"
]);

const PERMISSION_TOOLS: ReadonlySet<string> = new Set([
  ToolName.AskPermission, ToolName.Confirm, ToolName.Elicitation,
]);

export function getToolCategory(name: string): ToolCategory {
  const lowerName = name?.toLowerCase()?.trim() || "";
  if (TERMINAL_TOOLS.has(lowerName)) return "terminal";
  if (TODO_TOOLS.has(lowerName)) return "todo";
  if (FILE_TOOLS.has(lowerName)) return "file";
  if (SEARCH_TOOLS.has(lowerName)) return "search";
  if (LIST_TOOLS.has(lowerName)) return "list";
  if (AI_TOOLS.has(lowerName)) return "ai";
  if (PERMISSION_TOOLS.has(lowerName)) return "permission";
  return "default";
}

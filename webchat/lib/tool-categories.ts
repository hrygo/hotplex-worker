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
} as const;

export type ToolCategory = "terminal" | "file" | "search" | "permission" | "default";

const TERMINAL_TOOLS: ReadonlySet<string> = new Set([
  ToolName.RunCommand, ToolName.Bash, ToolName.ExecuteCommand, ToolName.Shell,
]);

const FILE_TOOLS: ReadonlySet<string> = new Set([
  ToolName.EditFile, ToolName.WriteFile, ToolName.ReplaceFileContent,
  ToolName.CreateFile, ToolName.ApplyDiff,
  "write", "write_to_file", "multi_replace_file_content", "edit", "str_replace_editor"
]);

const SEARCH_TOOLS: ReadonlySet<string> = new Set([
  ToolName.GrepSearch, ToolName.ViewFile, ToolName.SearchFiles,
  ToolName.ReadFile, ToolName.ListDirectory,
]);

const PERMISSION_TOOLS: ReadonlySet<string> = new Set([
  ToolName.AskPermission, ToolName.Confirm, ToolName.Elicitation,
]);

export function getToolCategory(name: string): ToolCategory {
  if (TERMINAL_TOOLS.has(name)) return "terminal";
  if (FILE_TOOLS.has(name)) return "file";
  if (SEARCH_TOOLS.has(name)) return "search";
  if (PERMISSION_TOOLS.has(name)) return "permission";
  return "default";
}

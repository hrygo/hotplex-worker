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
} as const;

export type ToolCategory = "terminal" | "file" | "search" | "list" | "permission" | "default";

const TERMINAL_TOOLS: ReadonlySet<string> = new Set([
  ToolName.RunCommand, ToolName.Bash, ToolName.ExecuteCommand, ToolName.Shell,
]);

const FILE_TOOLS: ReadonlySet<string> = new Set([
  ToolName.EditFile, ToolName.WriteFile, ToolName.ReplaceFileContent,
  ToolName.CreateFile, ToolName.ApplyDiff,
  ToolName.Write, ToolName.WriteToFile, ToolName.MultiReplaceFileContent,
  ToolName.Edit, ToolName.StrReplaceEditor,
]);

const SEARCH_TOOLS: ReadonlySet<string> = new Set([
  ToolName.GrepSearch, ToolName.ViewFile, ToolName.SearchFiles,
  ToolName.ReadFile,
]);

const LIST_TOOLS: ReadonlySet<string> = new Set([
  ToolName.ListDirectory,
]);

const PERMISSION_TOOLS: ReadonlySet<string> = new Set([
  ToolName.AskPermission, ToolName.Confirm, ToolName.Elicitation,
]);

export function getToolCategory(name: string): ToolCategory {
  const lowerName = name?.toLowerCase()?.trim() || "";
  if (TERMINAL_TOOLS.has(lowerName)) return "terminal";
  if (FILE_TOOLS.has(lowerName)) return "file";
  if (SEARCH_TOOLS.has(lowerName)) return "search";
  if (LIST_TOOLS.has(lowerName)) return "list";
  if (PERMISSION_TOOLS.has(lowerName)) return "permission";
  return "default";
}

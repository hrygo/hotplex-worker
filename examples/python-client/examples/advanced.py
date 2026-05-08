import asyncio
import logging
import os
import sys
from datetime import datetime
from typing import Any, Dict

from rich.console import Console
from rich.live import Live
from rich.markdown import Markdown
from rich.panel import Panel
from rich.table import Table
from rich.status import Status
from rich.progress import Progress, SpinnerColumn, TextColumn

from hotplex_client import (
    HotPlexClient,
    WorkerType,
    SessionState,
)
from hotplex_client.types import (
    MessageDeltaData,
    MessageStartData,
    MessageEndData,
    ToolCallData,
    PermissionRequestData,
    DoneData,
    StateData,
    ReasoningData,
    ErrorData,
)

# Configure logging for internal client debugging if needed
logging.basicConfig(level=logging.WARNING)
console = Console()

class HotPlexAdvancedDemo:
    """
    A comprehensive demonstration of HotPlex Gateway capabilities.
    """

    def __init__(self, url: str):
        self.url = url
        self.full_content = ""
        self.reasoning_content = ""
        self._live: Live | None = None
        self._status: Status | None = None

    def _get_display_panel(self):
        # Create a layout with reasoning (if any) and main content
        parts = []
        if self.reasoning_content:
            parts.append(Panel(
                self.reasoning_content, 
                title="[bold yellow]Thinking[/bold yellow]", 
                border_style="yellow",
                dim=True
            ))
        
        parts.append(Panel(
            Markdown(self.full_content or "Waiting for response..."),
            title="[bold blue]Assistant[/bold blue]",
            border_style="blue",
            padding=(1, 2)
        ))
        
        return Panel(
            asyncio.gather(*[asyncio.sleep(0)]) and "\n".join([str(p) for p in parts]) if False else parts[0] if len(parts) == 1 else "\n".join([str(p) for p in parts]), # This is a placeholder for actual rich layout logic
            title="[bold]HotPlex Live Session[/bold]"
        )

    def _update_live(self):
        if self._live:
            # We use a Group to show multiple panels
            from rich.console import Group
            parts = []
            if self.reasoning_content:
                parts.append(Panel(
                    self.reasoning_content, 
                    title="[bold yellow]Thinking[/bold yellow]", 
                    border_style="yellow",
                    dim=True
                ))
            parts.append(Panel(
                Markdown(self.full_content or "..."),
                title="[bold blue]Assistant[/bold blue]",
                border_style="blue",
                padding=(1, 2)
            ))
            self._live.update(Group(*parts))

    async def run_session(self, prompt: str, session_id: str | None = None, config: Dict[str, Any] | None = None):
        console.print(f"\n[bold green]>>> Prompt:[/bold green] {prompt}")
        
        self.full_content = ""
        self.reasoning_content = ""
        
        # Initialize client with optional config (e.g. specialized model or environment vars)
        # Config can include things like: {"anthropic_api_key": "...", "model": "claude-3-opus-20240229"}
        async with HotPlexClient(
            url=self.url,
            worker_type=WorkerType.CLAUDE_CODE,
            session_id=session_id,
            config=config
        ) as client:
            
            curr_sid = client.session_id
            console.print(f"[dim]Connected | Session: {curr_sid} | Worker: {WorkerType.CLAUDE_CODE}[/dim]")

            with Live(refresh_per_second=10) as live:
                self._live = live
                self._update_live()

                # --- 1. Content Handling ---
                @client.on("message.start")
                async def on_start(data: MessageStartData):
                    logger.info(f"Message started: {data.id} (role: {data.role})")

                @client.on("message.delta")
                async def on_delta(data: MessageDeltaData):
                    self.full_content += data.content
                    self._update_live()

                @client.on("reasoning")
                async def on_reasoning(data: ReasoningData):
                    self.reasoning_content += data.content
                    self._update_live()

                # --- 2. Tool & Permission Orchestration ---
                @client.on("tool_call")
                async def on_tool_call(data: ToolCallData):
                    # In advanced apps, you might have a Tool Registry here
                    console.print(f"\n[bold yellow]🔧 Tool Call:[/bold yellow] [cyan]{data.name}[/cyan]")
                    console.print(f"  [dim]Input: {data.input}[/dim]")
                    
                    # Simulated Tool Execution
                    status_text = f"Executing {data.name}..."
                    with console.status(status_text):
                        await asyncio.sleep(1.5) # Simulate work
                        
                        # Mocking different tool results
                        if data.name == "get_weather":
                            result = {"temp": 22, "condition": "Sunny", "location": data.input.get("location")}
                        elif data.name == "run_command":
                            result = {"stdout": "Successfully ran command", "exit_code": 0}
                        else:
                            result = {"status": "success", "message": "Tool executed by local handler"}
                    
                    await client.send_tool_result(data.id, result)
                    console.print(f"  [bold green]✓ Result returned to Gateway[/bold green]")

                @client.on("permission_request")
                async def on_permission(data: PermissionRequestData):
                    console.print(Panel(
                        f"[bold red]Security Check[/bold red]\n"
                        f"The AI wants to use: [cyan]{data.tool_name}[/cyan]\n"
                        f"Description: {data.description or 'No description provided'}\n"
                        f"Args: {data.args or 'None'}",
                        title="Permission Required",
                        border_style="red"
                    ))
                    
                    # Real-world app would wait for user keypress
                    # For demo, we auto-approve after a slight delay
                    console.print("  [italic yellow]Analyzing request safety...[/italic yellow]")
                    await asyncio.sleep(2)
                    
                    await client.send_permission_response(data.id, allowed=True, reason="User approved via advanced handler")
                    console.print("  [bold green]✓ Permission Granted[/bold green]")

                # --- 3. Lifecycle & Error Management ---
                @client.on("state")
                async def on_state(data: StateData):
                    # Log state transitions for observability
                    logger.info(f"State transition: {data.state} | {data.message or ''}")

                @client.on("error")
                async def on_error(data: ErrorData):
                    console.print(Panel(
                        f"[bold]Code:[/bold] {data.code}\n"
                        f"[bold]Message:[/bold] {data.message}\n"
                        f"[dim]Details: {data.details}[/dim]",
                        title="[red]Gateway Error[/red]",
                        border_style="red"
                    ))

                # --- 4. Execution ---
                try:
                    await client.send_input(prompt)
                    
                    # wait_for_done returns the DoneData which contains final stats
                    done_info = await client.wait_for_done(timeout=300)
                    
                    self._update_live() # Final update
                    self._show_summary(done_info)
                    return curr_sid
                except asyncio.TimeoutError:
                    console.print("[bold red]Timeout reached waiting for response[/bold red]")
                    return curr_sid

    def _show_summary(self, data: DoneData):
        table = Table(title="Session Summary", box=None, padding=(0, 2))
        table.add_column("Metric", style="dim")
        table.add_column("Value", style="bold")

        stats = data.stats
        if stats:
            table.add_row("Outcome", "[green]Success[/green]" if data.success else "[red]Failure[/red]")
            if stats.duration_ms:
                table.add_row("Latency", f"{stats.duration_ms} ms")
            if stats.total_tokens:
                table.add_row("Token Usage", f"{stats.total_tokens} (Prompt: {stats.input_tokens}, Comp: {stats.output_tokens})")
            if stats.cost_usd:
                table.add_row("Estimated Cost", f"${stats.cost_usd:.5f}")
            if stats.model:
                table.add_row("Worker Model", stats.model)
        
        console.print(table)

async def main():
    url = os.getenv("HOTPLEX_URL", "ws://localhost:8888")
    demo = HotPlexAdvancedDemo(url)
    
    try:
        # Example 1: Multi-turn conversation with memory (Session Resume)
        sid = await demo.run_session(
            "What's the weather in Tokyo? (Please use a tool if possible)",
            config={"temperature": 0.7}
        )
        
        if sid:
            console.print("\n[bold cyan]─── CONTINUING SESSION ───[/bold cyan]")
            await asyncio.sleep(2)
            await demo.run_session(
                "Convert that temperature to Fahrenheit.",
                session_id=sid
            )
            
    except Exception as e:
        console.print(f"[bold red]System Error:[/bold red] {e}")
        sys.exit(1)

if __name__ == "__main__":
    try:
        asyncio.run(main())
    except KeyboardInterrupt:
        console.print("\n[red]Session aborted by user.[/red]")

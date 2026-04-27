use std::collections::VecDeque;
use std::io::{self, Write};
use std::time::{Duration, Instant};

use alloy_provider::{Provider, ProviderBuilder, WsConnect};
use clap::Parser;
use color_eyre::eyre::{self, WrapErr};
use owo_colors::OwoColorize;
use owo_colors::Stream::{Stderr, Stdout};
use owo_colors::Style;
use tokio::time::timeout;
use tokio_stream::StreamExt;

#[derive(Parser)]
#[command(about = "Monitor EVM block times via HTTP polling or WebSocket subscription")]
struct Args {
    /// HTTP(S) or WS(S) RPC URL
    #[arg(short, long, env = "RPC_URL")]
    url: String,

    /// Window size for running stats
    #[arg(short, long, default_value_t = 64)]
    window: usize,

    /// Poll interval in milliseconds (HTTP mode only)
    #[arg(short, long, default_value_t = 200)]
    poll_ms: u64,

    /// WebSocket only: verify WSS upgrade and JSON-RPC framing by subscribing to
    /// new heads (`eth_subscribe`), print one block number, and exit successfully.
    #[arg(long)]
    smoke: bool,

    /// With `--smoke`: max seconds to wait for the first `newHeads` notification after subscribing.
    #[arg(long, default_value_t = 60, value_parser = clap::value_parser!(u64).range(1..=86400))]
    smoke_timeout_secs: u64,
}

struct Stats {
    window: usize,
    samples: VecDeque<f64>,
    total: f64,
}

impl Stats {
    fn new(window: usize) -> Self {
        if window == 0 {
            panic!("window size must be non-zero")
        }
        Self {
            window,
            samples: VecDeque::new(),
            total: 0.0,
        }
    }

    fn push(&mut self, val: f64) {
        if self.samples.len() == self.window {
            self.total -= self.samples.pop_front().unwrap();
        }
        self.samples.push_back(val);
        self.total += val;
    }

    fn mean(&self) -> f64 {
        if self.samples.is_empty() {
            return 0.0;
        }
        self.total / self.samples.len() as f64
    }

    fn median(&self) -> f64 {
        if self.samples.is_empty() {
            return 0.0;
        }
        let mut sorted: Vec<f64> = self.samples.iter().copied().collect();
        sorted.sort_by(f64::total_cmp);
        let n = sorted.len();
        if n.is_multiple_of(2) {
            (sorted[n / 2 - 1] + sorted[n / 2]) / 2.0
        } else {
            sorted[n / 2]
        }
    }
}

fn print_header(url: &str, window: usize, mode: &str) {
    println!(
        "{} {}  {}",
        mode.if_supports_color(Stdout, |s| s.style(Style::new().bold().cyan())),
        url.if_supports_color(Stdout, |s| s.dimmed()),
        format!("(window={window})").if_supports_color(Stdout, |s| s.dimmed()),
    );
    println!(
        "{}",
        format!(
            "{:<10} {:>12} {:>14} {:>14}",
            "block", "latest (s)", "avg (s)", "median (s)"
        )
        .if_supports_color(Stdout, |s| s.bold()),
    );
    println!(
        "{}",
        "-".repeat(54).if_supports_color(Stdout, |s| s.dimmed()),
    );
}

fn record_block(block: u64, stats: &mut Stats, last_seen: &mut Option<Instant>) {
    let now = Instant::now();
    if let Some(prev) = *last_seen {
        let elapsed = prev.elapsed().as_secs_f64();
        stats.push(elapsed);
        let mean = stats.mean();

        let elapsed_fmt = format!("{:>12.3}", elapsed);
        let elapsed_colored = if elapsed <= mean {
            elapsed_fmt
                .if_supports_color(Stdout, |s| s.green())
                .to_string()
        } else {
            elapsed_fmt
                .if_supports_color(Stdout, |s| s.red())
                .to_string()
        };

        print!(
            "\r{} {} {} {}",
            format!("{:<10}", block).if_supports_color(Stdout, |s| s.yellow()),
            elapsed_colored,
            format!("{:>14.3}", mean).if_supports_color(Stdout, |s| s.cyan()),
            format!("{:>14.3}", stats.median()).if_supports_color(Stdout, |s| s.cyan()),
        );
    } else {
        print!(
            "\r{} {:>12} {:>14} {:>14}",
            format!("{:<10}", block).if_supports_color(Stdout, |s| s.yellow()),
            "—",
            "—",
            "—",
        );
    }
    io::stdout().flush().ok();
    *last_seen = Some(now);
}

async fn run_http(args: &Args) -> eyre::Result<()> {
    let poll = Duration::from_millis(args.poll_ms);

    let provider = ProviderBuilder::new()
        .connect(&args.url)
        .await
        .wrap_err("failed to connect to rpc")?;

    let mut stats = Stats::new(args.window);
    let mut last_block: Option<u64> = None;
    let mut last_seen: Option<Instant> = None;

    print_header(&args.url, args.window, "Polling");
    println!(
        "{}",
        format!("poll={}ms", args.poll_ms).if_supports_color(Stdout, |s| s.dimmed()),
    );

    loop {
        match provider.get_block_number().await {
            Ok(block) => {
                if Some(block) != last_block {
                    record_block(block, &mut stats, &mut last_seen);
                    last_block = Some(block);
                }
            }
            Err(e) => {
                let msg = e.to_string();
                if msg.contains("429") || msg.contains("Too Many Requests") {
                    eprintln!(
                        "\n{}",
                        format!(
                            "[rate limited] HTTP 429 — lower --poll-ms (currently {}ms) and retry",
                            args.poll_ms
                        )
                        .if_supports_color(Stderr, |s| s.style(Style::new().bold().red())),
                    );
                    std::process::exit(1);
                }
                eprint!(
                    "\r{}",
                    format!("[poll error] {e}").if_supports_color(Stderr, |s| s.red()),
                );
                io::stderr().flush().ok();
            }
        }

        tokio::time::sleep(poll).await;
    }
}

async fn run_ws_smoke(args: &Args) -> eyre::Result<()> {
    let provider = ProviderBuilder::new()
        .connect_ws(WsConnect::new(&args.url))
        .await
        .wrap_err("failed to connect via WebSocket")?;

    let sub = provider
        .subscribe_blocks()
        .await
        .wrap_err("failed to subscribe to blocks (eth_subscribe / newHeads)")?;

    let mut stream = sub.into_stream();
    let wait = Duration::from_secs(args.smoke_timeout_secs);
    let header = timeout(wait, stream.next())
        .await
        .map_err(|_| {
            eyre::eyre!(
                "timed out after {}s waiting for first newHeads from subscription",
                args.smoke_timeout_secs
            )
        })?
        .ok_or_else(|| eyre::eyre!("[ws] subscription stream ended before first block"))?;

    println!(
        "ok: WSS + JSON-RPC subscription works (block number {})",
        header.number
    );
    Ok(())
}

async fn run_ws(args: &Args) -> eyre::Result<()> {
    let provider = ProviderBuilder::new()
        .connect_ws(WsConnect::new(&args.url))
        .await
        .wrap_err("failed to connect via WebSocket")?;

    let sub = provider
        .subscribe_blocks()
        .await
        .wrap_err("failed to subscribe to blocks")?;

    let mut stream = sub.into_stream();
    let mut stats = Stats::new(args.window);
    let mut last_seen: Option<Instant> = None;

    print_header(&args.url, args.window, "Subscribing");

    while let Some(header) = stream.next().await {
        record_block(header.number, &mut stats, &mut last_seen);
    }

    eyre::bail!("[ws] subscription stream ended unexpectedly");
}

#[tokio::main]
async fn main() -> eyre::Result<()> {
    color_eyre::install()?;
    let args = Args::parse();

    if args.smoke && !args.url.starts_with("ws://") && !args.url.starts_with("wss://") {
        eprintln!(
            "{}",
            "error: --smoke is only supported with ws:// or wss:// URLs"
                .if_supports_color(Stderr, |s| s.style(Style::new().bold().red())),
        );
        std::process::exit(1);
    }

    if args.url.starts_with("ws://") || args.url.starts_with("wss://") {
        if args.smoke {
            run_ws_smoke(&args).await
        } else {
            run_ws(&args).await
        }
    } else if args.url.starts_with("http://") || args.url.starts_with("https://") {
        run_http(&args).await
    } else {
        eprintln!(
            "{}",
            "error: URL must start with http://, https://, ws://, or wss://"
                .if_supports_color(Stderr, |s| s.style(Style::new().bold().red())),
        );
        std::process::exit(1);
    }
}

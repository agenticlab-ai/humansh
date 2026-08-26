import { CopyInstallButton } from "./copy-install-button";

const githubUrl = "https://github.com/agenticlab-ai/humansh";
const installCommand =
  "curl -fsSL https://humansh.com/install | bash";

const trustHighlights = [
  {
    index: "01",
    eyebrow: "No replacement terminal",
    title: "Keep the setup you know",
    body: "HumanSH works inside supported Zsh and Bash prompts in iTerm, Terminal.app, VS Code's terminal, and more.",
  },
  {
    index: "02",
    eyebrow: "No extra cloud service",
    title: "One provider request",
    body: "An ordinary translation talks only to your chosen AI provider. The HumanSH CLI sends no telemetry or analytics.",
  },
  {
    index: "03",
    eyebrow: "No broad machine access",
    title: "Your machine stays yours",
    body: "No files, shell history, environment variables, or credentials are sent in a translation request.",
  },
];

const safetyFeatures = [
  {
    number: "01",
    title: "Nothing runs before you approve it",
    body: "The generated command lands in your editable prompt. Read it, change it, cancel it, or press Enter to run it.",
  },
  {
    number: "02",
    title: "Risk gets a deliberate pause",
    body: "Commands with destructive patterns require a stronger confirmation instead of an ordinary Enter.",
  },
  {
    number: "03",
    title: "Only a small request leaves",
    body: "HumanSH sends your request and a small amount of system context—not your files, history, secrets, or command output.",
  },
  {
    number: "04",
    title: "The provider gets an empty workspace",
    body: "Translations run in an isolated disposable directory with a minimal environment and tools disabled where supported.",
  },
  {
    number: "05",
    title: "No product tracking layer",
    body: "The HumanSH CLI does not collect telemetry, create a cloud account, or add a separate analytics request.",
  },
  {
    number: "06",
    title: "No surprise paid fallback",
    body: "OpenRouter's metered API is optional and explicit. HumanSH never silently switches you to it.",
  },
];

const faqs = [
  {
    question: "Do I need to switch terminal apps?",
    answer:
      "No. HumanSH integrates with your shell prompt, so you can keep using iTerm, Terminal.app, VS Code's terminal, or another terminal you prefer. It currently supports Zsh and Bash 4.3+ on macOS and Linux.",
  },
  {
    question: "Does HumanSH run generated commands automatically?",
    answer:
      "No. It inserts the command into your editable prompt for review. You decide whether to edit, cancel, or run it. Higher-risk commands require an additional deliberate confirmation.",
  },
  {
    question: "What leaves my machine during a translation?",
    answer:
      "By default: your English request, the target command environment, operating-system and architecture details, a privacy-normalized folder label, and a fixed list of detected tools. Files, file contents, shell history, environment variables, credentials, usernames, and command output are not sent.",
  },
  {
    question: "Do I need another paid AI API?",
    answer:
      "No. HumanSH can use an existing supported ChatGPT, Claude, or Cursor subscription through its current login. OpenRouter's metered API is an optional path and is never selected silently.",
  },
  {
    question: "Is HumanSH free?",
    answer:
      "Yes. HumanSH is open source and costs nothing to install or use. Bring a supported subscription you already have, or explicitly choose OpenRouter's metered API.",
  },
  {
    question: "What happens when I type a real command?",
    answer:
      "HumanSH classifies clear commands locally and leaves them alone, so they run normally without a provider request. Genuinely ambiguous input is preserved rather than guessed or sent.",
  },
];

function Brand() {
  return (
    <a className="brand" href="#top" aria-label="HumanSH home">
      <span className="brandMark" aria-hidden="true">
        <span>%</span>
        <i />
      </span>
      <span>HumanSH</span>
    </a>
  );
}

function Arrow() {
  return <span aria-hidden="true">↗</span>;
}

export default function Home() {
  return (
    <>
      <a className="skipLink" href="#main">
        Skip to content
      </a>

      <header className="siteHeader">
        <div className="container headerInner">
          <Brand />
          <nav className="navLinks" aria-label="Primary navigation">
            <a href="#how-it-works">How it works</a>
            <a href="#privacy">Privacy</a>
            <a href="#install">Install</a>
          </nav>
          <a className="headerGithub" href={githubUrl} target="_blank" rel="noreferrer" data-analytics-event="github_open" data-analytics-placement="header_repo">
            GitHub <Arrow />
          </a>
        </div>
      </header>

      <main id="main">
        <section className="hero" id="top">
          <div className="heroGlow heroGlowLavender" aria-hidden="true" />
          <div className="heroGlow heroGlowBlue" aria-hidden="true" />
          <div className="container heroGrid">
            <div className="heroCopy">
              <p className="eyebrow"><span /> No replacement terminal</p>
              <h1>Use plain English in the terminal you already know.</h1>
              <p className="heroLead">
                HumanSH works inside the Zsh or Bash prompt you already use—in iTerm, Terminal.app, VS Code&apos;s terminal, and more. No replacement terminal or unfamiliar interface: describe, review, and run in place.
              </p>
              <div className="heroActions">
                <a className="button buttonPrimary" href="#install">
                  Install HumanSH <span aria-hidden="true">↓</span>
                </a>
                <a className="button buttonGhost" href={githubUrl} target="_blank" rel="noreferrer" data-analytics-event="github_open" data-analytics-placement="hero_repo">
                  View the source <Arrow />
                </a>
              </div>
              <p className="heroNote"><span aria-hidden="true">✓</span> Free and open source. Nothing runs until you approve it.</p>
            </div>

            <div className="demoWrap" id="demo">
              <div className="demoHalo" aria-hidden="true" />
              <figure className="demoCard">
                <div className="demoToolbar">
                  <span className="windowDots" aria-hidden="true"><i /><i /><i /></span>
                  <span>Your terminal</span>
                  <span className="demoStatus"><i /> in flow</span>
                </div>
                <img
                  className="demoGif"
                  src="/humansh-demo.gif"
                  width="960"
                  height="600"
                  alt="HumanSH turns an English request into a command in the terminal, waits for review, then runs it after Enter is pressed."
                />
                <div className="demoStatic" aria-hidden="true">
                  <p><span>%</span> show me which process is listening on port 3000</p>
                  <p><span>%</span> lsof -nP -iTCP:3000 -sTCP:LISTEN<i /></p>
                  <small>Generated command. Review it, then press Enter.</small>
                </div>
                <figcaption>
                  <span><i aria-hidden="true" /> Plain English in</span>
                  <span className="captionArrow" aria-hidden="true">→</span>
                  <span><i aria-hidden="true" /> Reviewable command out</span>
                </figcaption>
              </figure>
              <div className="floatingLabel floatingLabelTop">No tab switching</div>
            </div>
          </div>
        </section>

        <section className="trustStrip" aria-label="HumanSH trust highlights">
          <div className="container trustGrid">
            {trustHighlights.map((item) => (
              <article className="trustItem" key={item.index}>
                <span className="trustIndex">{item.index}</span>
                <div>
                  <p>{item.eyebrow}</p>
                  <h2>{item.title}</h2>
                  <span>{item.body}</span>
                </div>
              </article>
            ))}
          </div>
        </section>

        <section className="section painSection" aria-labelledby="pain-title">
          <div className="container">
            <div className="sectionHeading splitHeading">
              <div>
                <p className="sectionKicker">The interruption you know</p>
                <h2 id="pain-title">The command is not the hard part. Leaving your work is.</h2>
              </div>
              <p>
                You are already in the terminal when memory fails. The expensive part is the detour: open a chatbot, rebuild the context, wait, copy, switch back, paste.
              </p>
            </div>

            <div className="flowCompare">
              <article className="flowPanel oldFlow">
                <div className="flowPanelHead">
                  <span>Without HumanSH</span>
                  <strong>2 apps · 6 steps</strong>
                </div>
                <div className="flowVisual oldFlowVisual">
                  <div className="miniWindow terminalWindow">
                    <div className="miniBar"><span>Terminal</span><span>stuck</span></div>
                    <div className="miniBody"><span>%</span> which flags did I need…<i /></div>
                  </div>
                  <div className="switchPill">⌘ Tab</div>
                  <div className="miniWindow chatWindow">
                    <div className="miniBar"><span>Chatbot</span><span>new chat</span></div>
                    <div className="chatBody">
                      <p>Which command shows what is using port 3000?</p>
                      <span>lsof -nP -iTCP:3000…</span>
                    </div>
                  </div>
                </div>
                <ol className="flowSteps" aria-label="Workflow without HumanSH">
                  <li>Forget</li><li>Switch</li><li>Ask</li><li>Wait</li><li>Copy</li><li>Paste</li>
                </ol>
              </article>

              <article className="flowPanel newFlow">
                <div className="flowPanelHead">
                  <span>With HumanSH</span>
                  <strong>Same terminal · 3 steps</strong>
                </div>
                <div className="flowVisual newFlowVisual">
                  <div className="miniWindow terminalWindow largeTerminalWindow">
                    <div className="miniBar"><span>Your usual terminal</span><span className="liveLabel"><i /> in flow</span></div>
                    <div className="miniBody newFlowBody">
                      <p><span>%</span> show which process is using port 3000</p>
                      <p><span>%</span> lsof -nP -iTCP:3000 -sTCP:LISTEN<i /></p>
                      <small>Review, then press Enter.</small>
                    </div>
                  </div>
                </div>
                <ol className="flowSteps" aria-label="Workflow with HumanSH">
                  <li>Describe</li><li>Review</li><li>Run</li>
                </ol>
              </article>
            </div>
          </div>
        </section>

        <section className="section howSection" id="how-it-works" aria-labelledby="how-title">
          <div className="container howGrid">
            <div className="sectionHeading stickyHeading">
              <p className="sectionKicker">How it works</p>
              <h2 id="how-title">One request. Two Enters. You stay in control.</h2>
              <p>HumanSH keeps the important pause between generating a command and running it.</p>
            </div>

            <div className="stepStack">
              <article className="stepCard">
                <span className="stepNumber">01</span>
                <div><p className="stepVerb">Describe</p><h3>Type what you want to do</h3><p>Use the words already in your head. There is no prompt template to learn.</p></div>
                <div className="stepTerminal"><span>%</span> find the five largest files here<i /></div>
              </article>
              <article className="stepCard">
                <span className="stepNumber">02</span>
                <div><p className="stepVerb">Review</p><h3>The command appears in place</h3><p>Nothing has run. Inspect it, edit it, or cancel it just like anything else in your prompt.</p></div>
                <div className="stepTerminal commandReady"><span>%</span> find . -type f -exec du -h &#123;&#125; + | sort -rh | head -n 5<i /></div>
              </article>
              <article className="stepCard">
                <span className="stepNumber">03</span>
                <div><p className="stepVerb">Run</p><h3>Press Enter when it looks right</h3><p>The reviewed command runs in the terminal you were already using.</p></div>
                <div className="resultRows" aria-label="Example command output"><i /><i /><i /></div>
              </article>
            </div>
          </div>
        </section>

        <section className="section quietSection" aria-labelledby="quiet-title">
          <div className="container">
            <div className="sectionHeading centeredHeading">
              <p className="sectionKicker">Useful when needed. Invisible when not.</p>
              <h2 id="quiet-title">It knows when to stay quiet.</h2>
              <p>A local decision happens before any provider request.</p>
            </div>
            <div className="decisionGrid">
              <article className="decisionCard literalCard">
                <div className="decisionTop"><span>Clear command</span><strong>Runs normally</strong></div>
                <code><b>%</b> git status</code>
                <p>No added wait. No provider request.</p>
              </article>
              <article className="decisionCard languageCard">
                <div className="decisionTop"><span>Plain English</span><strong>Translated for review</strong></div>
                <code><b>%</b> undo the last commit but keep my changes</code>
                <p>The command replaces your request—without running.</p>
              </article>
              <article className="decisionCard ambiguousCard">
                <div className="decisionTop"><span>Genuinely ambiguous</span><strong>Left untouched</strong></div>
                <code><b>%</b> status report</code>
                <p>HumanSH refuses to guess or send it.</p>
              </article>
            </div>
          </div>
        </section>

        <section className="section privacySection" id="privacy" aria-labelledby="privacy-title">
          <div className="privacyGlow" aria-hidden="true" />
          <div className="container">
            <div className="sectionHeading privacyHeading">
              <p className="sectionKicker">Private by restraint</p>
              <h2 id="privacy-title">The shortest path needs the smallest footprint.</h2>
              <p>HumanSH is designed to do one job, ask for little, and leave control where it belongs—with you.</p>
            </div>
            <div className="safetyGrid">
              {safetyFeatures.map((feature) => (
                <article className="safetyCard" key={feature.number}>
                  <span>{feature.number}</span>
                  <h3>{feature.title}</h3>
                  <p>{feature.body}</p>
                </article>
              ))}
            </div>
            <a className="textLink lightLink" href={`${githubUrl}/blob/main/docs/security.md`} target="_blank" rel="noreferrer" data-analytics-event="github_open" data-analytics-placement="privacy_security">
              Read the complete security model <Arrow />
            </a>
          </div>
        </section>

        <section className="section compatibilitySection" aria-labelledby="compatibility-title">
          <div className="container compatibilityGrid">
            <div className="sectionHeading">
              <p className="sectionKicker">Current compatibility</p>
              <h2 id="compatibility-title">Works with the AI you already use.</h2>
              <p>Provider names belong here, after the product is understood—not in the promise.</p>
            </div>
            <div className="providerPanel">
              <div className="providerRow"><span className="providerDot">C</span><div><strong>ChatGPT subscription</strong><p>Use your existing ChatGPT login through Codex.</p></div><span>Existing login</span></div>
              <div className="providerRow"><span className="providerDot">A</span><div><strong>Claude subscription</strong><p>Use your existing claude.ai login.</p></div><span>Existing login</span></div>
              <div className="providerRow"><span className="providerDot">↗</span><div><strong>Cursor subscription</strong><p>Use your existing Cursor browser login.</p></div><span>Existing login</span></div>
              <div className="providerFootnote"><span>Optional</span><p>OpenRouter&apos;s metered API is available as an explicit fourth path. HumanSH never falls back to it silently.</p></div>
            </div>
          </div>
        </section>

        <section className="section installSection" id="install" aria-labelledby="install-title">
          <div className="container installCard">
            <div className="installCopy">
              <p className="sectionKicker">Open source · MIT licensed</p>
              <h2 id="install-title">Stay in flow the next time memory fails.</h2>
              <p>Install HumanSH, follow the guided setup, and describe your first command in plain English.</p>
              <div className="installLinks">
                <a className="button buttonLight" href={`${githubUrl}#install`} target="_blank" rel="noreferrer" data-analytics-event="github_open" data-analytics-placement="install_guide">Installation guide <Arrow /></a>
                <a className="textLink lightLink" href={githubUrl} target="_blank" rel="noreferrer" data-analytics-event="github_open" data-analytics-placement="install_repo">View on GitHub <Arrow /></a>
              </div>
            </div>
            <div className="installTerminal">
              <div className="installTerminalHead">
                <span className="windowDots" aria-hidden="true"><i /><i /><i /></span>
                <span className="installTerminalTitle">Install HumanSH</span>
                <CopyInstallButton command={installCommand} />
              </div>
              <pre><code><span>$</span> {installCommand}</code></pre>
              <div className="installMeta"><span><i /> No sudo</span><span><i /> Checksum verified</span><span><i /> Guided setup</span></div>
            </div>
          </div>
        </section>

        <section className="section faqSection" aria-labelledby="faq-title">
          <div className="container faqGrid">
            <div className="sectionHeading">
              <p className="sectionKicker">Questions, answered</p>
              <h2 id="faq-title">The important details.</h2>
            </div>
            <div className="faqList">
              {faqs.map((faq) => (
                <details key={faq.question}>
                  <summary>{faq.question}<span aria-hidden="true">+</span></summary>
                  <p>{faq.answer}</p>
                </details>
              ))}
            </div>
          </div>
        </section>
      </main>

      <footer className="siteFooter">
        <div className="container footerTop">
          <div><Brand /><p>Plain English in. Reviewable commands out.</p></div>
          <div className="footerLinks">
            <a href={`${githubUrl}/blob/main/README.md`} target="_blank" rel="noreferrer" data-analytics-event="github_open" data-analytics-placement="footer_docs">Docs <Arrow /></a>
            <a href={`${githubUrl}/blob/main/docs/security.md`} target="_blank" rel="noreferrer" data-analytics-event="github_open" data-analytics-placement="footer_security">Security <Arrow /></a>
            <a href={githubUrl} target="_blank" rel="noreferrer" data-analytics-event="github_open" data-analytics-placement="footer_repo">GitHub <Arrow /></a>
          </div>
        </div>
        <div className="container footerBottom"><span>© 2026 HumanSH</span><span>MIT licensed · Website counts aggregate visits and selected actions without cookies or user IDs</span></div>
      </footer>
    </>
  );
}

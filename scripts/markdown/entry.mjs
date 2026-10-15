import MarkdownIt from 'markdown-it';
import anchor from 'markdown-it-anchor';
import footnote from 'markdown-it-footnote';
import deflist from 'markdown-it-deflist';
import { full as emoji } from 'markdown-it-emoji';
import tasks from 'markdown-it-task-lists';
import abbr from 'markdown-it-abbr';
import sub from 'markdown-it-sub';
import sup from 'markdown-it-sup';
import mark from 'markdown-it-mark';
import ins from 'markdown-it-ins';
import texmath from 'markdown-it-texmath';
import katex from 'katex';
import mermaid from 'mermaid';
import hljs from 'highlight.js';
import DOMPurify from 'dompurify';

const config = await (await fetch('/document.json')).json();
const main = document.querySelector('main');
const warnings = [];
try {
  document.documentElement.style.fontSize = `${14 * config.scale}px`;
  for (const [key, value] of Object.entries(config.colors)) document.documentElement.style.setProperty(`--${key}`, value);
  const escape = value => value.replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
  const formulas = [];
  const mathEngine = {renderToString(source, options) {
    const index = formulas.push({source, displayMode:!!options?.displayMode})-1;
    return `<span class="math-placeholder" data-formula="${index}"></span>`;
  }};
  const md = new MarkdownIt({html:true, linkify:true, highlight(code, language) {
    if (language === 'mermaid') return `<pre class="mermaid-source">${escape(code)}</pre>`;
    if (language === 'math') return `<pre class="math-source">${escape(code)}</pre>`;
    return language && hljs.getLanguage(language) ? hljs.highlight(code, {language, ignoreIllegals:true}).value : '';
  }});
  // Older extension releases use markdown-it's former assign helper.
  md.utils = {...md.utils, assign:Object.assign, has:(object,key)=>Object.prototype.hasOwnProperty.call(object,key)};
  md.use(anchor).use(footnote).use(deflist).use(emoji).use(tasks).use(abbr).use(sub).use(sup).use(mark).use(ins)
    .use(texmath, {engine:mathEngine, delimiters:['dollars','brackets'], katexOptions:{trust:false, strict:'ignore', throwOnError:false}});
  md.inline.ruler.before('math_inline', 'github_math', (state, silent) => {
    if (!state.src.startsWith('$`', state.pos)) return false;
    const end = state.src.indexOf('`$', state.pos+2);
    if (end < 0) return false;
    if (!silent) state.push('github_math', 'math', 0).content = state.src.slice(state.pos+2, end);
    state.pos = end+2;
    return true;
  });
  md.renderer.rules.github_math = (tokens, index) => mathEngine.renderToString(tokens[index].content, {displayMode:false});
  md.core.ruler.after('inline', 'github_references', state => {
    let repository;
    try { const parts = new URL(config.baseURL).pathname.split('/'); repository = `https://github.com/${parts[1]}/${parts[2]}`; } catch { return; }
    for (const block of state.tokens) {
      if (block.type !== 'inline' || !block.children) continue;
      const children = []; let inLink = 0;
      for (const token of block.children) {
        if (token.type === 'link_open') inLink++;
        if (token.type === 'link_close') inLink--;
        if (token.type !== 'text' || inLink) { children.push(token); continue; }
        const pattern = /(^|[\s(])(@[a-z\d](?:[a-z\d-]{0,38})|#\d+)\b/gi;
        let previous = 0;
        for (const match of token.content.matchAll(pattern)) {
          const start = match.index+match[1].length;
          const text = new state.Token('text', '', 0); text.content = token.content.slice(previous, start); children.push(text);
          const open = new state.Token('link_open', 'a', 1);
          open.attrSet('href', match[2][0] === '@' ? `https://github.com/${match[2].slice(1)}` : `${repository}/issues/${match[2].slice(1)}`);
          const label = new state.Token('text', '', 0); label.content = match[2];
          children.push(open, label, new state.Token('link_close', 'a', -1));
          previous = start+match[2].length;
        }
        const tail = new state.Token('text', '', 0); tail.content = token.content.slice(previous); children.push(tail);
      }
      block.children = children;
    }
  });
  for (const tag of ['th_open','td_open']) md.renderer.rules[tag] = (tokens,index,options,env,self) => {
    const alignment = tokens[index].attrGet('style')?.match(/text-align:\s*(left|center|right)/)?.[1];
    if (alignment) tokens[index].attrSet('align', alignment);
    return self.renderToken(tokens,index,options);
  };
  const renderedHTML = md.render(config.source);
  main.innerHTML = DOMPurify.sanitize(renderedHTML, {
    ADD_TAGS:['eq','eqn'], ADD_ATTR:['align'],
    FORBID_TAGS:['script','style','iframe','object','embed','form','video','audio','input','button','textarea','select'],
    // Task checkboxes are inert display controls; restore them below from
    // markdown-it's generated markup, never execute user-provided controls.
    FORBID_ATTR:['style','srcset'],
  });
  // The sanitizer removes input elements. Display task state as a Unicode box.
  const taskStates = [...renderedHTML.matchAll(/<input class="task-list-item-checkbox"[^>]*>/g)].map(m => /\bchecked\b/.test(m[0]));
  main.querySelectorAll('.task-list-item').forEach((item, index) => item.prepend(document.createTextNode(taskStates[index] ? '☑ ' : '☐ ')));
  const alertTitles = {NOTE:'Note',TIP:'Tip',IMPORTANT:'Important',WARNING:'Warning',CAUTION:'Caution'};
  for (const quote of main.querySelectorAll('blockquote')) {
    const paragraph = quote.firstElementChild;
    if (paragraph?.tagName !== 'P') continue;
    const match = paragraph.textContent.match(/^\[!(NOTE|TIP|IMPORTANT|WARNING|CAUTION)\]/);
    if (!match) continue;
    const first = paragraph.firstChild;
    if (first?.nodeType !== Node.TEXT_NODE || !first.textContent.startsWith(match[0])) continue;
    first.textContent = first.textContent.slice(match[0].length).trimStart();
    quote.className = `markdown-alert markdown-alert-${match[1].toLowerCase()}`;
    const title = document.createElement('p'); title.className = 'markdown-alert-title'; title.textContent = alertTitles[match[1]]; quote.prepend(title);
  }
  const mathOptions = {trust:false, strict:'ignore', throwOnError:false, displayMode:true};
  for (const node of main.querySelectorAll('.math-placeholder')) {
    const formula = formulas[Number(node.dataset.formula)];
    if (formula) katex.render(formula.source, node, {...mathOptions,displayMode:formula.displayMode});
  }
  for (const node of main.querySelectorAll('.math-source')) {
    const replacement = document.createElement('div'); replacement.className = 'math-block';
    katex.render(node.textContent, replacement, mathOptions); node.replaceWith(replacement);
  }
  mermaid.initialize({startOnLoad:false, securityLevel:'strict', suppressErrorRendering:true,
    theme:config.dark ? 'dark':'default', maxTextSize:50000, maxEdges:1000,
    htmlLabels:false, flowchart:{htmlLabels:false}, themeVariables:{fontFamily:'sans-serif',fontSize:`${16*config.scale}px`}});
  let diagramIndex = 0;
  for (const node of main.querySelectorAll('.mermaid-source')) {
    try {
      const {svg} = await mermaid.render(`diagram-${diagramIndex++}`, node.textContent);
      const replacement = document.createElement('div'); replacement.className = 'mermaid';
      // Mermaid's strict mode sanitizes its SVG/HTML labels itself. A second
      // generic SVG pass would remove foreignObject labels in some diagram types.
      replacement.innerHTML = svg;
      node.replaceWith(replacement);
    } catch (error) {
      warnings.push('Mermaid: '+String(error.message || error).slice(0,200));
      node.classList.add('render-error');
      node.prepend(document.createTextNode('Mermaid 图表解析失败，原文如下：\n'));
    }
  }
  const details = [...main.querySelectorAll('details')];
  details.forEach((node, index) => { if (config.details) node.open = config.details.includes(index); });
  for (const node of main.querySelectorAll('a[href],img[src]')) {
    const attribute = node.tagName === 'IMG' ? 'src':'href';
    const raw = node.getAttribute(attribute);
    if (attribute === 'href' && raw.startsWith('#')) continue;
    try {
      const link = new URL(raw, config.baseURL);
      if (attribute === 'href' && link.protocol === 'mailto:') { node.setAttribute(attribute,link.href); continue; }
      if (!['https:','http:'].includes(link.protocol) || link.username || link.password) node.removeAttribute(attribute);
      else node.setAttribute(attribute, link.href);
    } catch { node.removeAttribute(attribute); }
  }
  await Promise.race([Promise.all([...main.querySelectorAll('img')].map(image => image.decode().catch(() => {}))), new Promise(resolve => setTimeout(resolve,5000))]);
  // Freeze incomplete images before measuring; no late image may change layout.
  for (const image of main.querySelectorAll('img')) if (!image.complete || !image.naturalWidth) image.replaceWith(document.createTextNode(image.alt || '[图片未加载]'));
  await document.fonts.ready;
  // Math and diagram content scales with the app's DPI just like body text.
  for (const svg of main.querySelectorAll('.mermaid > svg')) {
    svg.style.maxWidth = '100%'; svg.style.height = 'auto';
  }
  await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
  const links = [];
  const addRects = (node, url) => {
    for (const rect of node.getClientRects()) if (rect.width && rect.height) links.push({url,x:rect.x,y:rect.y,width:rect.width,height:rect.height});
  };
  for (const node of main.querySelectorAll('a[href]')) addRects(node, node.getAttribute('href'));
  details.forEach((node, index) => { const summary = node.querySelector(':scope > summary'); if (summary) addRects(summary, `details:${index}`); });
  const anchors = {};
  for (const node of main.querySelectorAll('[id]')) {
    anchors[node.id] = node.getBoundingClientRect().top;
    try { anchors[decodeURIComponent(node.id)] = anchors[node.id]; } catch {}
  }
  window.renderedDocument = {width:config.width, height:Math.max(20,Math.ceil(main.getBoundingClientRect().height)),links,anchors,
    details:details.flatMap((node,index) => node.open ? [index]:[]),warnings};
  window.markdownReady = true;
} catch (error) {
  window.markdownError = String(error.message || error).slice(0,500);
  window.markdownErrorStack = String(error.stack || error);
}

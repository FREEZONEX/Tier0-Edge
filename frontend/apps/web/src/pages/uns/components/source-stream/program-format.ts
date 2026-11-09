import { javascriptLanguage } from '@codemirror/lang-javascript';
import { js as beautify } from 'js-beautify';

function syntaxSignature(code: string): string | undefined {
  const parts: string[] = [];
  let invalid = false;
  javascriptLanguage.parser.parse(code).iterate({
    enter(node) {
      if (node.type.isError) invalid = true;
      parts.push(node.name);
      if (!node.node.firstChild && !['LineComment', 'BlockComment'].includes(node.name))
        parts.push(code.slice(node.from, node.to).trim());
    },
    leave() {
      parts.push('/');
    },
  });
  return invalid ? undefined : JSON.stringify(parts);
}

// 只解析和排版，绝不执行脚本；语法或 ASI 结构变化时保留原文。
export function formatProgramScript(code: string): { code: string; valid: boolean } {
  if (!code.trim()) return { code, valid: true };
  try {
    const signature = syntaxSignature(code);
    if (!signature) return { code, valid: false };
    const formatted = beautify(code, {
      indent_size: 2,
      wrap_line_length: 100,
      preserve_newlines: true,
      end_with_newline: true,
      unescape_strings: false,
    });
    return syntaxSignature(formatted) === signature ? { code: formatted, valid: true } : { code, valid: false };
  } catch {
    return { code, valid: false };
  }
}

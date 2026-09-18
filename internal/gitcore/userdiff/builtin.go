package userdiff

import "github.com/oops1/gogit/internal/gitcore/posixre"

const wordTail = "|[^[:space:]]|[\xc0-\xff][\x80-\xbf]+"

func patterns(name, funcname, words string) Driver {
	return Driver{
		Name:      name,
		FuncName:  Pattern{Source: funcname, Flags: posixre.Extended},
		WordRegex: words + wordTail,
	}
}

func ipatterns(name, funcname, words string) Driver {
	driver := patterns(name, funcname, words)
	driver.FuncName.Flags |= posixre.IgnoreCase
	return driver
}

var builtinDrivers = []Driver{
	ipatterns("ada",
		"!^(.*[ \t])?(is[ \t]+new|renames|is[ \t]+separate)([ \t].*)?$\n"+
			"!^[ \t]*with[ \t].*$\n"+
			"^[ \t]*((procedure|function)[ \t]+.*)$\n"+
			"^[ \t]*((package|protected|task)[ \t]+.*)$",
		"[a-zA-Z][a-zA-Z0-9_]*|[-+]?[0-9][0-9#_.aAbBcCdDeEfF]*([eE][+-]?[0-9_]+)?|=>|\\.\\.|\\*\\*|:=|/=|>=|<=|<<|>>|<>"),
	patterns("bash",
		"^[ \t]*((([a-zA-Z_][a-zA-Z0-9_]*[ \t]*\\([ \t]*\\))|(function[ \t]+[a-zA-Z_][a-zA-Z0-9_]*(([ \t]*\\([ \t]*\\))|([ \t]+)))).*$)",
		"[a-zA-Z_][a-zA-Z0-9_]*|\\$[a-zA-Z0-9_]+|\\$\\{|\\|\\||&&|<<|>>|==|!=|<=|>=|[-+*/%&|^]=|:=|:-|:\\+|:\\?|##|%%|\\^\\^|,,|[-a-zA-Z0-9_]+|\\(|\\)|\\{|\\}|\\[|\\]"),
	patterns("bibtex",
		"(@[a-zA-Z]{1,}[ \t]*\\{{0,1}[ \t]*[^ \t\"@',\\#}{~%]*).*$",
		"[={}\"]|[^={}\" \t]+"),
	patterns("cpp",
		"!^[ \t]*[A-Za-z_][A-Za-z_0-9]*:[[:space:]]*($|/[/*])\n"+
			"^((::[[:space:]]*)?[A-Za-z_].*)$",
		"[a-zA-Z_][a-zA-Z0-9_]*|[0-9][0-9.]*([Ee][-+]?[0-9]+)?[fFlLuU]*|0[xXbB][0-9a-fA-F]+[lLuU]*|\\.[0-9][0-9]*([Ee][-+]?[0-9]+)?[fFlL]?|[-+*/<>%&^|=!]=|--|\\+\\+|<<=?|>>=?|&&|\\|\\||::|->\\*?|\\.\\*|<=>"),
	patterns("csharp",
		"!(^|[ \t]+)(do|while|for|foreach|if|else|new|default|return|switch|case|throw|catch|using|lock|fixed)([ \t(]+|$)\n"+
			"^[ \t]*(([][[:alnum:]@_.](<[][[:alnum:]@_, \t<>]+>)?)+([ \t]+([][[:alnum:]@_.](<[][[:alnum:]@_, \t<>]+>)?)+)+[ \t]*\\([^;]*)$\n"+
			"^[ \t]*(([][[:alnum:]@_.](<[][[:alnum:]@_, \t<>]+>)?)+([ \t]+([][[:alnum:]@_.](<[][[:alnum:]@_, \t<>]+>)?)+)+[^;=:,()]*)$\n"+
			"^[ \t]*(((static|public|internal|private|protected|new|unsafe|sealed|abstract|partial)[ \t]+)*(class|enum|interface|struct|record)[ \t]+.*)$\n"+
			"^[ \t]*(namespace[ \t]+.*)$",
		"[a-zA-Z_][a-zA-Z0-9_]*|[-+0-9.e]+[fFlL]?|0[xXbB]?[0-9a-fA-F]+[lL]?|[-+*/<>%&^|=!]=|--|\\+\\+|<<=?|>>=?|&&|\\|\\||::|->"),
	ipatterns("css",
		"![:;][[:space:]]*$\n"+
			"^[:[@.#]?[_a-z0-9].*$",
		"-?[_a-zA-Z][-_a-zA-Z0-9]*|-?[0-9]+|\\#[0-9a-fA-F]+"),
	patterns("dts",
		"!;\n"+
			"!=\n"+
			"^[ \t]*((/[ \t]*\\{|&?[a-zA-Z_]).*)",
		"[a-zA-Z0-9,._+?#-]+|[-+*/%&^|!~]|>>|<<|&&|\\|\\|"),
	patterns("elixir",
		"^[ \t]*((def(macro|module|impl|protocol|p)?|test)[ \t].*)$",
		"[@:]?[a-zA-Z0-9@_?!]+|[-+]?0[xob][0-9a-fA-F]+|[-+]?[0-9][0-9_.]*([eE][-+]?[0-9_]+)?|:?(\\+\\+|--|\\.\\.|~~~|<>|\\^\\^\\^|<?\\|>|<<<?|>?>>|<<?~|~>?>|<~>|<=|>=|===?|!==?|=~|&&&?|\\|\\|\\|?|=>|<-|\\\\\\\\|->)|:?%[A-Za-z0-9_.]\\{\\}?"),
	ipatterns("fortran",
		"!^([C*]|[ \t]*!)\n"+
			"!^[ \t]*MODULE[ \t]+PROCEDURE[ \t]\n"+
			"^[ \t]*((END[ \t]+)?(PROGRAM|MODULE|BLOCK[ \t]+DATA|([^!'\" \t]+[ \t]+)*(SUBROUTINE|FUNCTION))[ \t]+[A-Z].*)$",
		"[a-zA-Z][a-zA-Z0-9_]*|\\.([Ee][Qq]|[Nn][Ee]|[Gg][TtEe]|[Ll][TtEe]|[Tt][Rr][Uu][Ee]|[Ff][Aa][Ll][Ss][Ee]|[Aa][Nn][Dd]|[Oo][Rr]|[Nn]?[Ee][Qq][Vv]|[Nn][Oo][Tt])\\.|[-+]?[0-9.]+([AaIiDdEeFfLlTtXx][Ss]?[-+]?[0-9.]*)?(_[a-zA-Z0-9][a-zA-Z0-9_]*)?|//|\\*\\*|::|[/<>=]="),
	ipatterns("fountain",
		"^((\\.[^.]|(int|ext|est|int\\.?/ext|i/e)[. ]).*)$",
		"[^ \t-]+"),
	patterns("golang",
		"^[ \t]*(func[ \t]*.*(\\{[ \t]*)?)\n"+
			"^[ \t]*(type[ \t].*(struct|interface)[ \t]*(\\{[ \t]*)?)",
		"[a-zA-Z_][a-zA-Z0-9_]*|[-+0-9.eE]+i?|0[xX]?[0-9a-fA-F]+i?|[-+*/<>%&^|=!:]=|--|\\+\\+|<<=?|>>=?|&\\^=?|&&|\\|\\||<-|\\.{3}"),
	patterns("html",
		"^[ \t]*(<[Hh][1-6]([ \t].*)?>.*)$",
		"[^<>= \t]+"),
	patterns("ini",
		"^[ \t]*\\[[^]]+\\]",
		"[^ \t]+"),
	patterns("java",
		"!^[ \t]*(catch|do|for|if|instanceof|new|return|switch|throw|while)\n"+
			"^[ \t]*(([a-z-]+[ \t]+)*(class|enum|interface|record)[ \t]+.*)$\n"+
			"^[ \t]*(([A-Za-z_<>&][][?&<>.,A-Za-z_0-9]*[ \t]+)+[A-Za-z_][A-Za-z_0-9]*[ \t]*\\([^;]*)$",
		"[a-zA-Z_][a-zA-Z0-9_]*|[-+0-9.e]+[fFlL]?|0[xXbB]?[0-9a-fA-F]+[lL]?|[-+*/<>%&^|=!]=|--|\\+\\+|<<=?|>>>?=?|&&|\\|\\|"),
	patterns("kotlin",
		"^[ \t]*(([a-z]+[ \t]+)*(fun|class|interface)[ \t]+.*)$",
		"[a-zA-Z_][a-zA-Z0-9_]*|0[xXbB][0-9a-fA-F_]+[lLuU]*|[0-9][0-9_]*([.][0-9_]*)?([Ee][-+]?[0-9]+)?[fFlLuU]*|[.][0-9][0-9_]*([Ee][-+]?[0-9]+)?[fFlLuU]?|[-+*/<>%&^|=!]==?|--|\\+\\+|<<=|>>=|&&|\\|\\||->|\\.\\*|!!|[?:.][.:]"),
	patterns("markdown",
		"^ {0,3}#{1,6}[ \t].*",
		"[^<>= \t]+"),
	patterns("matlab",
		"^[[:space:]]*((classdef|function)[[:space:]].*)$|^(%%%?|##)[[:space:]].*$",
		"[a-zA-Z_][a-zA-Z0-9_]*|[-+0-9.e]+|[=~<>]=|\\.[*/\\^']|\\|\\||&&"),
	patterns("objc",
		"!^[ \t]*(do|for|if|else|return|switch|while)\n"+
			"^[ \t]*([-+][ \t]*\\([ \t]*[A-Za-z_][A-Za-z_0-9* \t]*\\)[ \t]*[A-Za-z_].*)$\n"+
			"^[ \t]*(([A-Za-z_][A-Za-z_0-9]*[ \t]+)+[A-Za-z_][A-Za-z_0-9]*[ \t]*\\([^;]*)$\n"+
			"^(@(implementation|interface|protocol)[ \t].*)$",
		"[a-zA-Z_][a-zA-Z0-9_]*|[-+0-9.e]+[fFlL]?|0[xXbB]?[0-9a-fA-F]+[lL]?|[-+*/<>%&^|=!]=|--|\\+\\+|<<=?|>>=?|&&|\\|\\||::|->"),
	patterns("pascal",
		"^(((class[ \t]+)?(procedure|function)|constructor|destructor|interface|implementation|initialization|finalization)[ \t]*.*)$\n"+
			"^(.*=[ \t]*(class|record).*)$",
		"[a-zA-Z_][a-zA-Z0-9_]*|[-+0-9.e]+|0[xXbB]?[0-9a-fA-F]+|<>|<=|>=|:=|\\.\\."),
	patterns("perl",
		"^package .*\n"+
			"^sub [[:alnum:]_':]+[ \t]*(\\([^)]*\\)[ \t]*)?(:[^;#]*)?(\\{[ \t]*)?(#.*)?$\n"+
			"^(BEGIN|END|INIT|CHECK|UNITCHECK|AUTOLOAD|DESTROY)[ \t]*(\\{[ \t]*)?(#.*)?$\n"+
			"^=head[0-9] .*",
		"[[:alpha:]_'][[:alnum:]_']*|0[xb]?[0-9a-fA-F_]*|[0-9a-fA-F_]+(\\.[0-9a-fA-F_]+)?([eE][-+]?[0-9_]+)?|=>|-[rwxoRWXOezsfdlpSugkbctTBMAC>]|~~|::|&&=|\\|\\|=|//=|\\*\\*=|&&|\\|\\||//|\\+\\+|--|\\*\\*|\\.\\.\\.?|[-+*/%.^&<>=!|]=|=~|!~|<<|<>|<=>|>>"),
	patterns("php",
		"^[\t ]*(((public|protected|private|static|abstract|final)[\t ]+)*function.*)$\n"+
			"^[\t ]*((((final|abstract)[\t ]+)?class|enum|interface|trait).*)$",
		"[a-zA-Z_][a-zA-Z0-9_]*|[-+0-9.e]+|0[xXbB]?[0-9a-fA-F]+|[-+*/<>%&^|=!.]=|--|\\+\\+|<<=?|>>=?|===|&&|\\|\\||::|->"),
	patterns("python",
		"^[ \t]*((class|(async[ \t]+)?def)[ \t].*)$",
		"[a-zA-Z_][a-zA-Z0-9_]*|[-+0-9.e]+[jJlL]?|0[xX]?[0-9a-fA-F]+[lL]?|[-+*/<>%&^|=!]=|//=?|<<=?|>>=?|\\*\\*=?"),
	patterns("r",
		"^[ \t]*([a-zA-z][a-zA-Z0-9_.]*[ \t]*(<-|=)[ \t]*function.*)$",
		"[^ \t]+"),
	patterns("ruby",
		"^[ \t]*((class|module|def)[ \t].*)$",
		"(@|@@|\\$)?[a-zA-Z_][a-zA-Z0-9_]*|[-+0-9.e]+|0[xXbB]?[0-9a-fA-F]+|\\?(\\\\C-)?(\\\\M-)?.|//=?|[-+*/<>%&^|=!]=|<<=?|>>=?|===|\\.{1,3}|::|[!=]~"),
	patterns("rust",
		"^[\t ]*((pub(\\([^\\)]+\\))?[\t ]+)?((async|const|unsafe|extern([\t ]+\"[^\"]+\"))[\t ]+)?(struct|enum|union|mod|trait|fn|impl|macro_rules!)[< \t]+[^;]*)$",
		"[a-zA-Z_][a-zA-Z0-9_]*|[0-9][0-9_a-fA-Fiosuxz]*(\\.([0-9]*[eE][+-]?)?[0-9_fF]*)?|[-+*\\/<>%&^|=!:]=|<<=?|>>=?|&&|\\|\\||->|=>|\\.{2}=|\\.{3}|::"),
	patterns("scheme",
		"^(\\(.*)$\n"+
			"^[\t ]*(\\(((define|def(struct|syntax|class|method|rules|record|proto|alias)?)[-*/ \t]|(library|module|struct|class)[*+ \t]).*)$\n"+
			"^  ?(\\([Dd][Ee][Ff].*)$",
		"\\|([^|\\\\]|\\\\.)*\\||([^][)(}{ \t])+"),
	patterns("tex",
		"^(\\\\((sub)*section|chapter|part)\\*{0,1}\\{.*)$",
		"\\\\[a-zA-Z@]+|\\\\.|([a-zA-Z0-9]|[^\x01-\x7f])+"),
}

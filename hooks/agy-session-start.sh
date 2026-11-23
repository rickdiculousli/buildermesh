#!/bin/sh
command -v bmesh >/dev/null 2>&1 || { printf '{}\n'; exit 0; }
{ bmesh hosts --here; bmesh update --notice 2>/dev/null; } | awk '
NF {
  gsub(/\\/, "\\\\")
  gsub(/"/, "\\\"")
  gsub(/\t/, "\\t")
  gsub(/\r/, "")
  out = out (out != "" ? "\\n" : "") $0
}
END {
  if (out != "") {
    printf "{\"injectSteps\":[{\"systemMessage\":{\"systemMessage\":\"%s\"}}]}\n", out
  } else {
    printf "{}\n"
  }
}
'

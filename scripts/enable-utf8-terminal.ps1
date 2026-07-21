$ErrorActionPreference = "Stop"

function Set-Utf8Terminal {
    [CmdletBinding()]
    param()

    $utf8NoBom = New-Object System.Text.UTF8Encoding($false)

    [Console]::InputEncoding = $utf8NoBom
    [Console]::OutputEncoding = $utf8NoBom
    # Keep the legacy Windows console code page aligned with .NET's UTF-8
    # encodings.  Without this, Windows PowerShell can still inherit Big5
    # (950), which garbles UTF-8 text rendered by native programs.
    chcp 65001 | Out-Null
    # $OutputEncoding is used by native-command pipelines.  Make the setting
    # survive after this function returns.
    $global:OutputEncoding = $utf8NoBom

    # Windows PowerShell 5.1 otherwise treats BOM-less UTF-8 files as the
    # system ANSI code page (Big5 on this machine), producing mojibake when
    # Codex reads UTF-8 documentation through Get-Content / Select-String.
    $PSDefaultParameterValues["Get-Content:Encoding"] = "utf8"
    $PSDefaultParameterValues["Select-String:Encoding"] = "utf8"

    if ($PSVersionTable.PSVersion.Major -lt 6) {
        $PSDefaultParameterValues["Out-File:Encoding"] = "utf8"
        $PSDefaultParameterValues["Set-Content:Encoding"] = "utf8"
        $PSDefaultParameterValues["Add-Content:Encoding"] = "utf8"
        $PSDefaultParameterValues["Export-Csv:Encoding"] = "utf8"
    }

    $env:PYTHONIOENCODING = "utf-8"
    $env:DOTNET_CLI_UI_LANGUAGE = "en-US"
    $env:LANG = "C.UTF-8"
    $env:LC_ALL = "C.UTF-8"
}

Set-Utf8Terminal

Write-Host "Terminal encoding normalized to UTF-8."

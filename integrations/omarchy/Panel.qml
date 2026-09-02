import QtQuick
import QtQuick.Controls
import Quickshell
import Quickshell.Io
import qs.Commons
import qs.Ui

Panel {
  id: root
  moduleName: "io.github.pabloduke.jot"
  manageIpc: false

  property var anchorItem: null
  property var hostWidget: null
  property string syncState: "idle"
  property string localError: ""
  property string processError: ""
  property string pendingNote: ""
  readonly property bool busy: saveProcess.running || syncProcess.running

  function open() {
    root.controller.show()
    Qt.callLater(function() { noteArea.forceActiveFocus() })
  }
  function close() { root.controller.hide() }
  function toggle() { if (root.opened) close(); else open() }
  function submit() {
    if (busy || syncState === "syncing") return
    if (noteArea.text.trim() === "") {
      localError = "Enter a note before saving."
      return
    }
    localError = ""
    processError = ""
    pendingNote = noteArea.text
    var args = ["jot", "add", "--stdin", "--json"]
    if (tagsField.text.trim() !== "") args.push("--tags", tagsField.text.trim())
    saveProcess.command = args
    saveProcess.stdinEnabled = true
    saveProcess.running = true
  }
  function startSync() {
    if (busy) return
    syncState = "syncing"
    processError = ""
    syncProcess.running = true
  }

  KeyboardPanel {
    id: panel
    anchorItem: root.anchorItem
    owner: root.hostWidget || root
    bar: root.bar
    open: root.opened
    focusTarget: noteArea
    contentWidth: panel.fittedContentWidth(Style.space(430))
    contentHeight: panel.fittedContentHeight(content.implicitHeight)

    Column {
      id: content
      width: parent.width
      spacing: Style.space(10)

      Text {
        text: "Jot"
        color: Color.popups.text
        font.family: root.bar ? root.bar.fontFamily : Style.font.family
        font.pixelSize: Style.font.title
        font.bold: true
      }

      ScrollView {
        width: parent.width
        height: Style.space(190)
        clip: true
        TextArea {
          id: noteArea
          placeholderText: "Write a note…"
          wrapMode: TextEdit.Wrap
          color: Color.popups.text
          selectionColor: Color.accent
          font.family: root.bar ? root.bar.fontFamily : Style.font.family
          font.pixelSize: Style.font.body
          padding: Style.space(10)
          background: BorderSurface {
            color: Style.controlFill(noteArea.activeFocus, noteArea.hovered, Color.popups.text, Color.accent)
            borderSpec: Border.controlSpec(noteArea.activeFocus ? "focus" : "normal", Color.popups.text, Color.accent)
            radius: Style.cornerRadius
          }
          Keys.onPressed: function(event) {
            if (event.key === Qt.Key_Escape) {
              root.close(); event.accepted = true
            } else if ((event.key === Qt.Key_Return || event.key === Qt.Key_Enter) && (event.modifiers & Qt.ControlModifier)) {
              root.submit(); event.accepted = true
            }
          }
        }
      }

      TextField {
        id: tagsField
        width: parent.width
        placeholderText: "Optional tags, comma-separated"
        foreground: Color.popups.text
        accent: Color.accent
        Keys.onPressed: function(event) {
          if (event.key === Qt.Key_Escape) { root.close(); event.accepted = true }
          else if ((event.key === Qt.Key_Return || event.key === Qt.Key_Enter) && (event.modifiers & Qt.ControlModifier)) { root.submit(); event.accepted = true }
        }
      }

      Text {
        visible: root.localError !== "" || root.syncState !== "idle"
        width: parent.width
        wrapMode: Text.WordWrap
        text: root.localError !== "" ? root.localError : (root.syncState === "syncing" ? "Saved locally. Syncing…" : "Saved locally, but sync failed: " + root.processError)
        color: root.localError !== "" || root.syncState === "error" ? Color.urgent : Color.accent
        font.family: root.bar ? root.bar.fontFamily : Style.font.family
        font.pixelSize: Style.font.caption
      }

      Row {
        spacing: Style.space(8)
        Button {
          text: saveProcess.running ? "Saving…" : "Save & Push"
          foreground: Color.popups.text
          accent: Color.accent
          bordered: true
          enabled: !root.busy && root.syncState !== "syncing"
          onClicked: root.submit()
        }
        Button {
          visible: root.syncState === "error"
          text: "Retry sync"
          foreground: Color.urgent
          accent: Color.urgent
          bordered: true
          enabled: !root.busy
          onClicked: root.startSync()
        }
      }
    }
  }

  Process {
    id: saveProcess
    environment: ({ JOT_NO_SYNC: "1" })
    stdinEnabled: true
    stderr: StdioCollector { id: saveStderr }
    onStarted: {
      write(root.pendingNote)
      root.pendingNote = ""
      stdinEnabled = false
    }
    onExited: function(code, status) {
      if (code === 0) {
        noteArea.text = ""
        tagsField.text = ""
        root.close()
        Qt.callLater(root.startSync)
      } else {
        root.localError = String(saveStderr.text).trim() || "Could not save the note locally."
        stdinEnabled = true
        Qt.callLater(function() { noteArea.forceActiveFocus() })
      }
    }
  }

  Process {
    id: syncProcess
    command: ["jot", "sync"]
    stderr: StdioCollector { id: syncStderr }
    onExited: function(code, status) {
      if (code === 0) {
        root.syncState = "idle"
        root.processError = ""
      } else {
        root.syncState = "error"
        root.processError = String(syncStderr.text).trim() || "jot sync exited with an error"
      }
    }
  }
}

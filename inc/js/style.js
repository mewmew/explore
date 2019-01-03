// --- [ "server" code ] -------------------------------------------------------

// send_update_style_event sends an event to each frame, notifying that the
// styles have been updated. This indirection is used because same-origin policy
// prevent direct manupulation of the DOM of frames on the file:// scheme.
function send_update_style_event() {
	for (i = 0; i < frames.length; i++) {
		frames[i].window.postMessage("update_style", "*");
	}
}

// set_style sets the Chroma style to use.
function set_style(style) {
	localStorage.setItem("style", style);
	send_update_style_event();
}

// select_style updates the active Chroma based on the selected style.
function select_style() {
	var elem = document.getElementById("style_selection");
	var style = elem.value;
	set_style(style);
}

// update_style_selection updates the selected style to match the active one.
function update_style_selection() {
	var style = get_style();
	if (style !== null) {
		var elem = document.getElementById("style_selection");
		for (i = 0; i < elem.options.length; i++) {
			var option = elem.options[i];
			if (option.value == style) {
				option.selected = true;
			}
		}
	}
}

// --- [ "client" code ] -------------------------------------------------------

// add_update_style_event_listener adds an event listener to handle style update
// events.
function add_update_style_event_listener() {
	window.addEventListener("message", function(event) {
		if (event.data == "update_style") {
			update_style();
		}
	});
}

// update_style updates the active Chroma style.
function update_style() {
	var style = get_style();
	if (style !== null) {
		var cssName = "chroma_" + style + ".css";
		var cssPath = "inc/css/" + cssName;
		var elem = document.getElementById("chroma_style");
		elem.href = cssPath;
	}
}

// --- [ common ] --------------------------------------------------------------

// get_style returns the Chroma style in use.
function get_style() {
	return localStorage.getItem("style");
}

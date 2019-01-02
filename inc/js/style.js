// get_dir returns all but the last element of path, typically the path's
// directory.
function get_dir(path) {
	var end = path.lastIndexOf("/");
	return path.substr(0, end);
}

// set_style sets the Chroma style to use.
function set_style(style) {
	localStorage.setItem("style", style);
	update_style();
}

// get_style returns the Chroma style in use.
function get_style() {
	return localStorage.getItem("style");
}

// update_style updates the active Chroma style.
function update_style() {
	var style = get_style();
	if (style !== null) {
		var cssName = "chroma_" + style + ".css";
		var elems = find_elems("chroma_style");
		for (i = 0; i < elems.length; i++) {
			var elem = elems[i];
			var cssPath = get_dir(elem.href) + "/" + cssName;
			elem.href = cssPath;
		}
	}
}

// find_elems returns the elements of the given ID in the current document and
// all iframes.
function find_elems(id) {
	var elems = [];
	var elem = document.getElementById(id);
	if (elem !== null) {
		elems.push(elem);
	}
	for (i = 0; i < frames.length; i++) {
		var elem = frames[i].document.getElementById(id);
		if (elem !== null) {
			elems.push(elem);
		}
	}
	return elems;
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

// select_style updates the active Chroma based on the selected style.
function select_style() {
	var elem = document.getElementById("style_selection");
	var style = elem.value;
	set_style(style);
}

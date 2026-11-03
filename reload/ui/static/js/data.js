function escapeHtml(value) {
    return String(value == null ? "" : value).replace(/[&<>"']/g, function (character) {
        return {"&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;"}[character];
    });
}

var currentFileName = "";
$(document).ready(function(){
    $.ajax({
        url: "/monitoring/configs",
        dataType: "json",
        success: function (data) {
            $.each(data, function (index, units) {
                $("#dropdownItems").append("<li><a href='javascript:;' data-units='" + escapeHtml(units) + "'>" + escapeHtml(units) + "</a></li>");
            });
        },
        error: function (XMLHttpRequest, textStatus, errorThrown) {
            alert("error");
        }
    });

    $.ajax({
        url: "/monitoring/rules",
        dataType: "json",
        success: function (data) {
            $.each(data.data.groups, loadRules);
        },

        error: function (XMLHttpRequest, textStatus, errorThrown) {
            alert("error");
        }
    });

    $("#rules-items").click(
        function () {
           showRules();
        }
    );

    $("#btn-save").click(
        function () {
            $.ajax({
                dataType: "json",
                type: 'PUT',
                url: "monitoring/configs/" + encodeURI(currentFileName),
                data: JSON.stringify({content: editor.getValue()}),
                success: function (data) {
                    showRules();
                },
                error: function (XMLHttpRequest, textStatus, errorThrown) {
                    alert("error");
                }
            });
        }
    );
});

function showRules() {
    $("#rules").show();
    $("#rules-info").html('');
    $("#editor-container").hide();
    $.ajax({
        url: "/monitoring/rules",
        dataType: "json",
        success: function (data) {
            $.each(data.data.groups, loadRules);
        },

        error: function (XMLHttpRequest, textStatus, errorThrown) {
            alert("error");
        }
    });
}

function loadRules(index, group) {
    var hr =" <thead> <tr> <td colspan='3'><h2><a href=\"#\" name=\"go gc\"/>"+ escapeHtml(group.name) +"</h2></td>  </tr> </thead>"
        + "        <tbody>\n"
        + "        <tr>\n"
        + "            <td style=\"font-weight:bold\">Rule</td>\n"
        // + "            <td style=\"font-weight:bold\">State</td>\n"
        + "        </tr>"
        + "";

    var td = "";
    $.each(group.rules, function (i, rule) {
        td  +="<tr>\n"
            + "            <td class=\"rule_cell\">alert: <a href=\"#\">" + escapeHtml(rule.name) + "</a>\n"
            + "expr: <a href=\"#\">"+ escapeHtml(rule.query) + "</a>\n"
            + "for: "+ escapeHtml(rule.duration) + "\n"
            + "labels:\n"
            + "  env: " + escapeHtml(rule.labels.env) + "\n"
            + "  expr: " + escapeHtml(rule.labels.expr) + "\n"
            + "  level: " + escapeHtml(rule.labels.level) + "\n"
            + "annotations:\n"
            + "  description: " + escapeHtml(rule.annotations.description) + "\n"
            + "  summary: " + escapeHtml(rule.annotations.summary) + "\n"
            + "  value: " + escapeHtml(rule.annotations.value) + "\n"
            + "            </td>\n"
            // + "            <td class=\"state\">\n"
            // + "              <span class=\"alert alert-success state_indicator text-uppercase\">\n"
            // + "              "+ rule.health +"\n"
            // + "              </span>\n"
            // + "            </td>\n"
            + "        </tr>";
    });

    var tbody = hr + td + "</tbody>";
    $("#rules-info").append(tbody);
}

$(document).on('click', '#dropdownItems li a', function(e) {
    $("#rules").hide();
    $("#editor-container").show();
    currentFileName= $(e.target).data('units');
    $.ajax({
        url: "/monitoring/configs/" + $(e.target).data('units'),
        dataType: "json",
        success: function (data) {
            editor.setValue(data);
        },
        error: function (XMLHttpRequest, textStatus, errorThrown) {
            alert("error");
        }
    })
});
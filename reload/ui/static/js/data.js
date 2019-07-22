var currentFileName = "";
$(document).ready(function(){
    $.ajax({
        url: "/monitoring/configs",
        dataType: "json",
        success: function (data) {
            $.each(data, function (index, units) {
                $("#dropdownItems").append("<li><a href='javascript:;' data-units='" + units + "'>" + units + "</a></li>");
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
            $.each(data.data.groups, function (index, group) {
                var hr =" <thead> <tr> <td colspan='3'><h2><a href=\"#\" name=\"go gc\"/>"+ group.name +"</h2></td>  </tr> </thead>"
                    + "        <tbody>\n"
                    + "        <tr>\n"
                    + "            <td style=\"font-weight:bold\">Rule</td>\n"
                    + "            <td style=\"font-weight:bold\">State</td>\n"
                    + "        </tr>"
                    + "";

                var td = "";
                $.each(group.rules, function (i, rule) {
                    td  +="<tr>\n"
                        + "            <td class=\"rule_cell\">alert: <a href=\"#\">" + rule.name + "</a>\n"
                        + "expr: <a href=\"#\">"+ rule.query + "</a>\n"
                        + "for: "+ rule.duration + "\n"
                        + "labels:\n"
                        + "  env: " + rule.labels.env + "\n"
                        + "  expr: " + rule.labels.expr + "\n"
                        + "  level: " + rule.labels.level + "\n"
                        + "annotations:\n"
                        + "  description: " + rule.annotations.description + "\n"
                        + "  summary: " + rule.annotations.summary + "\n"
                        + "  value: " + rule.annotations.value + "\n"
                        + "            </td>\n"
                        + "            <td class=\"state\">\n"
                        + "              <span class=\"alert alert-success state_indicator text-uppercase\">\n"
                        + "              "+ rule.health +"\n"
                        + "              </span>\n"
                        + "            </td>\n"
                        + "        </tr>";
                });

                var tbody = hr + td + "</tbody>";
                $("#rules-info").append(tbody);
            });
        },

        error: function (XMLHttpRequest, textStatus, errorThrown) {
            console.log("ttt");
            alert("error");
        }
    });

    $("#rules-items").click(
        function () {
            $("#rules").show();
            $("#editor-container").hide();
        }
    );

    $("#btn-save").click(
        function () {
            $.ajax({
                dataType: "json",
                type: 'PUT',
                url: "monitoring/configs/" + encodeURI(currentFileName),
                data: {content: encodeURI(editor.getValue())},
                success: function (data) {
                    editor.setValue(data.content);
                }
            });
        }
    );
});

$(document).on('click', '#dropdownItems li a', function(e) {
    $("#rules").hide();
    $("#editor-container").show();
    currentFileName= $(e.target).data('units')
    $.ajax({
        url: "/monitoring/configs/" + $(e.target).data('units'),
        dataType: "json",
        success: function (data) {
            editor.setValue(data);
        }
    })
});
package main

import (
	"reflect"
	"testing"
	"testing/fstest"
)

func TestParseRelease(t *testing.T) {
	tree := fstest.MapFS{
		"src/webui/webapplication.h": {Data: []byte(`
inline const Utils::Version<3, 2> API_VERSION {2, 16, 2};
    const QHash<std::pair<QString, QString>, QString> m_allowedMethod =
    {
        {{u"torrents"_s, u"delete"_s}, Http::HEADER_REQUEST_METHOD_POST},
    };
`)},
		"src/webui/api/torrentscontroller.h": {Data: []byte(`
class TorrentsController : public APIController
{
private slots:
    void infoAction();
    void deleteAction() const;
};
`)},
		"src/webui/api/torrentscontroller.cpp": {Data: []byte(`
const QString PARAM_DELETE_FILES = u"deleteFiles"_s;

void TorrentsController::infoAction()
{
    setResult(u"[]"_s);
}

void TorrentsController::deleteAction() const
{
    requireParams({u"hashes"_s, PARAM_DELETE_FILES});
}
`)},
	}

	got, err := parseRelease(tree, "release-5.3.0")
	if err != nil {
		t.Fatal(err)
	}
	want := release{
		Version:       "v5.3.0",
		WebAPIVersion: "2.16.2",
		Actions: map[string]action{
			"torrents/info":   {Method: "any", Params: []string{}},
			"torrents/delete": {Method: "POST", Params: []string{"hashes", "deleteFiles"}},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestParseReleaseRejectsUnreadSource(t *testing.T) {
	const version = "inline const Utils::Version<3, 2> API_VERSION {2, 16, 2};\n"
	const cpp = "void TorrentsController::infoAction()\n{\n}\n"
	tests := map[string]struct{ app, header string }{
		"POST entry for an undeclared action": {
			app:    version + `m_allowedMethod = {{{u"torrents"_s, u"rename"_s}, Http::HEADER_REQUEST_METHOD_POST}};`,
			header: "    void infoAction();\n",
		},
		"POST entry the parser cannot read": {
			app: version + `m_allowedMethod = {
    {{u"torrents"_s, u"info"_s}, Http::HEADER_REQUEST_METHOD_POST},
    {{TORRENTS_SCOPE, u"stop"_s}, Http::HEADER_REQUEST_METHOD_POST},
};`,
			header: "    void infoAction();\n    void stopAction();\n",
		},
		"action declaration the parser cannot read": {
			app:    version,
			header: "    void infoAction();\n    void\n    stopAction();\n",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			tree := fstest.MapFS{
				"src/webui/webapplication.h":           {Data: []byte(tc.app)},
				"src/webui/api/torrentscontroller.h":   {Data: []byte(tc.header)},
				"src/webui/api/torrentscontroller.cpp": {Data: []byte(cpp)},
			}
			if _, err := parseRelease(tree, "release-5.3.0"); err == nil {
				t.Fatal("parseRelease returned no error")
			}
		})
	}
}

import re

from peewee import SQL, Field, NodeList


def _escape_wildcard(search_query):
    """
    Escapes the wildcards found in the given search query so that they are treated as *characters*
    rather than wildcards when passed to a LIKE or ILIKE clause with an ESCAPE '!'.
    """
    search_query = (
        search_query.replace("!", "!!").replace("%", "!%").replace("_", "!_").replace("[", "![")
    )

    # Just to be absolutely sure.
    search_query = search_query.replace("'", "")
    search_query = search_query.replace('"', "")
    search_query = search_query.replace("`", "")

    return search_query


def prefix_search(field, prefix_query):
    """
    Returns the wildcard match for searching for the given prefix query.
    """
    # Escape the known wildcard characters.
    prefix_query = _escape_wildcard(prefix_query)
    return Field.__pow__(field, NodeList((prefix_query + "%", SQL("ESCAPE '!'"))))


def match_like(field, search_query):
    """
    Generates a full-text match query using an ILIKE operation, which is needed for SQLite and
    Postgres.
    """
    escaped_query = _escape_wildcard(search_query)
    clause = NodeList(("%" + escaped_query + "%", SQL("ESCAPE '!'")))
    return Field.__pow__(field, clause)


def regex_search(query, field, pattern, offset, limit, matches=True):
    return (
        query.where(field.regexp(pattern)).offset(offset).limit(limit)
        if matches
        else query.where(~field.regexp(pattern)).offset(offset).limit(limit)
    )


def regex_sqlite(query, field, pattern, offset, limit, matches=True):
    # fetching all rows of the query here irrespective of limit and offset as sqlite does not support regexes.
    rows = query.execute()
    result = (
        [row for row in rows if re.search(pattern, getattr(row, field.name))]
        if matches
        else [row for row in rows if not re.search(pattern, getattr(row, field.name))]
    )
    return result[offset : offset + limit]
